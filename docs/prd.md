ssearch需求规格说明书
项目名称：ssearch
语言版本：Go 1.25+（无 CGO 依赖）
存储引擎：BoltDB（嵌入式 KV，单文件）

---

1. 项目总览与核心哲学

· 目标：跨平台（Win/Mac/Linux）纯 Go 单二进制 CLI，对硬盘文本类文件（OCR 产物、PDF 提取文本、日志、代码等）建立中文分词倒排索引，实现毫秒级关键词路径检索。
· 核心设计哲学：
  · 索引优先：接受首次建索引 >30 分钟，但检索必须毫秒级。
  · 增量极速：日常更新默认仅依赖文件大小（Size），不读内容。
  · MD5 裁决：仅在“大小撞车”或用户显式要求（--deep）时计算 MD5。
  · ID 驱动：使用数字 ID（uint64）主键，路径变更只需修改映射表（pathmap），倒排索引完全复用。
  · 零配置开箱即用：通过 .ssearch 目录自动发现项目根与数据目录。

---

2. 数据目录自动发现与约定（新增核心架构）

2.1 发现逻辑（运行时执行）

1. 向上查找：从当前工作目录（CWD）开始，向父目录链逐级查找名为 .ssearch 的隐藏目录。
2. 命中：若找到，则将 .ssearch 的父目录作为项目根（Project Root），.ssearch 本身作为数据目录（Data Dir）。
3. 未命中：若遍历至文件系统根仍未找到，则在 CWD 下自动创建 .ssearch 目录，并将 CWD 作为项目根。

2.2 默认路径映射（彻底简化 CLI）

资源 默认路径（相对于项目根） 说明
数据库文件 <项目根>/.ssearch/data.db 可通过 --db 覆盖
分词词典缓存 <项目根>/.ssearch/.mysearch_dict.gob 首次运行自动生成
配置文件（扩展名/停用词） <项目根>/.ssearch/.mysearch.toml 可选，与内置硬编码合并
项目根（扫描根） <项目根> 可通过 --root 覆盖

硬编码排除：遍历文件树时，自动跳过所有名为 .ssearch 的目录，防止工具索引自身数据。

---

3. 命令行接口（CLI v5.1 最终版）

3.1 全局参数（所有子命令共用）

· --db：指定数据库文件路径（默认：<数据目录>/data.db）。
· --include-hidden：包含以 . 开头的隐藏文件/目录（默认跳过）。

3.2 子命令定义（--root 已变为可选）

命令 用途 必选参数 可选参数（含默认值）
index 全量重建索引（清空旧库） 无（自动探测项目根） --root（覆盖自动探测） --max-size（默认 50MB） --stopwords（自定义停用词文件）
update 智能增量更新（日常高频） 无（自动探测项目根） --root（覆盖自动探测） --filter（指定文件或目录） --deep（校验 MD5） --force（无条件重建） --force-pending（重试待处理） --max-size（默认 50MB）
search 多关键词 AND 搜索 [keywords...] --limit N（默认 100，传 0 输出全部）
dedup 查找重复文件（基于 MD5） 无（自动探测项目根） --root（覆盖自动探测）
pending 查看待处理文件 无 --list / --clear
dead 查看永久放弃的文件 无 --list / --retry
compact 压缩 BoltDB 数据库文件 无 无

---

4. 数据库架构（BoltDB Schema）

数据库文件 data.db 包含以下 Buckets：

Bucket 名称 Key 类型 Value 类型 用途与说明
meta string string 存储 version（v5.1）、root（项目根绝对路径）。update/search 时校验一致性。
pathmap 文件 ID（uint64） 文件绝对路径（string） ID ↔ 路径映射。移动文件仅改此表。
filemeta 文件 ID（uint64） JSON：{Size, MD5, QuickHash, ModTime, RetryCount} 元数据。ModTime 变化时仅更新此字段，不重建索引。保留扩展字段（如 Owner、Permissions）以备未来扩展。
inverted 词项（string） 子 Bucket（内 Key=文件 ID） 倒排核心。每个词项独立子 Bucket，Key 集合即命中文件 ID 列表。
pending 文件路径（string） JSON：{Error, RetryCount} 待重试文件（超大/编码失败/被锁）。若重试时文件已不存在，直接从 pending 移除（视为已解决）。
dead 文件路径（string） JSON：{Error, LastAttempt} 重试超 3 次后永久放弃。

---

5. 核心算法更新（v5.1 增量变更）

5.1 文件遍历层（walker）统一约定

· 符号链接：index 与 update 行为完全一致——自动跳过所有子目录中的符号链接，不跟踪。
· 隐藏文件/目录：默认跳过以 . 开头的条目（除非 --include-hidden）。
· 自保护：硬编码跳过 .ssearch 目录、data.db 文件、.lock 文件及临时文件（*.tmp）。

5.2 增量更新（update）ModTime 处理逻辑

· 若路径存在于旧库且 Size 不变，但 ModTime 变化：
  · 仅更新 filemeta 中的 ModTime 字段。
  · 不触发索引重建（因为内容未变，只是元数据刷新）。
  · 此举保证了搜索排序（按 ModTime 降序）始终准确，且不产生额外 IO。

5.3 编码探测失败处理（双重保障）

1. 优先使用 chardet 探测编码并转 UTF-8。
2. 若 chardet 失败（返回 nil 或置信度 < 0.5）：
   · 尝试直接按 UTF-8 解码。
   · 若解码成功（无乱码替代字符），则采用该结果。
   · 若 UTF-8 解码也失败（包含无效字节序列），记入 pending 表。

5.4 配置文件合并（.mysearch.toml）

· 启动时在 <数据目录>/.mysearch.toml 查找配置文件（若存在）。
· 支持自定义：
  · 额外扩展名白名单（追加到硬编码列表）。
  · 自定义停用词表（覆盖内置，或与内置合并，取决于 TOML 字段设计）。
· 若文件不存在，静默使用硬编码默认值。

---

6. 词典缓存（dict.gob）生命周期

· 首次运行：gse 加载内置字典后，自动序列化为 <数据目录>/.mysearch_dict.gob。
· 后续启动：优先反序列化 .gob 文件（加载速度从 3s 降至 <50ms）。
· 版本更新：当 gse 库版本或内置字典变更时，自动检测并重建 .gob 缓存（通过比较版本号或字典文件 MD5）。

---

7. 错误处理与容错（已合并所有补充）

异常场景 处理策略（v5.1）
权限拒绝 / 路径过长 静默跳过，记录 Debug 日志。
文件被锁定（Windows） 跳过并记入 pending。
符号链接 子目录软链接统一跳过（根目录若为软链接则解析真实路径）。
移动检测 QuickHash（4KB）预筛选 + MD5 二次确认。
ModTime 变化 仅更新元数据，不重建索引。
数据库版本不匹配 启动时 panic 并提示运行 index。
Ctrl+C 中断 立即终止，BoltDB 事务自动回滚。
多实例并发写 无文件锁，由用户保证单实例。
搜索无匹配词 返回空结果集，stderr 输出“词 [xxx] 无匹配”。
停用词过滤 内置默认表 + --stopwords 自定义覆盖。
chardet 探测失败 备选 UTF-8 解码，仍失败则记入 pending。
pending 重试时文件已不存在 直接从 pending 移除（不进入 dead）。
数据库膨胀 提供 compact 子命令手动压缩。

---

8. 性能与规模预期（不变）

指标 数值
首次全量索引 可接受 >30 分钟。
增量更新（update） 仅遍历文件树（Stat），约 1~3 秒/10 万文件。
搜索响应 倒排索引查询，毫秒级（< 100ms）。
内存占用 常态 < 200MB；gse 加载字典约 100~150MB（缓存加速后启动 50ms）。

---

9. 最终冻结决策清单（35 项全覆盖）

序号 类别 最终决策
1 中文分词 gse（纯 Go，搜索模式）
2 符号链接 根目录解析真实路径，子目录跳过（walker 层统一）
3 移动检测 Size → QuickHash（4KB）→ MD5
4 数据库损坏 版本自检 + 强事务
5 权限/长路径 静默跳过，记录日志
6 HTML 处理 x/net/html 提取纯文本节点
7 超大文件 超 --max-size（默认 50MB）记入 pending
8 跨扩展名移动 视为移动
9 数据库膨胀 提供 compact 子命令
10 停用词 内置 + --stopwords 自定义
11 编码检测 chardet → UTF-8 备选 → pending
12 Ctrl+C 立即中断，事务回滚
13 无匹配词 返回空 + stderr 提示
14 词典加载 持久化 dict.gob 加速启动
15 倒排存储 每个词项独立子 Bucket
16 文件标识 数字 ID（uint64）+ pathmap
17 删除/移动索引更新 仅改 pathmap，倒排不变
18 历史缺失 MD5 补算；不存在则视为删除
19 全局 --db 默认 <数据目录>/data.db
20 --filter 语义 指定确定的文件或目录路径（非正则）
21 meta 表 记录 root 和 version
22 隐藏文件 默认跳过，--include-hidden 包含
23 避免索引自身 硬编码排除 .ssearch、data.db、.lock、*.tmp
24 搜索输出限制 --limit N，默认 100，0 表示全部
25 搜索结果排序 按 ModTime 降序（最新优先）
26 跨平台并发 无文件锁，用户保证单实例
27 扩展名配置 在数据目录查找 .mysearch.toml，与硬编码合并
28 ModTime 变化 仅更新元数据，不重建索引
29 pending 文件已删除 直接从 pending 移除
30 dict.gob 生成 首次运行自动生成，存放于数据目录
31 编码备选 chardet 失败后尝试 UTF-8 解码
32 项目根发现 向上查找 .ssearch，未找到则在 CWD 创建
33 --root 变为可选 默认使用自动探测的项目根
34 数据目录统一 所有工具文件（DB、缓存、配置）放于 .ssearch 内
35 硬编码排除 遍历时永远跳过 .ssearch 目录

---

10. 附录：项目目录结构（实施参照）

```
<项目根>/                               # 由 .ssearch 自动定位
├── .ssearch/                           # 隐藏数据目录（自动创建）
│   ├── data.db                         # BoltDB 数据库
│   ├── .mysearch_dict.gob              # gse 词典缓存
│   └── .mysearch.toml                  # 可选配置文件
├── (用户的其他文件/文件夹)
└── ...

mysearch/                               # 源码仓库
├── cmd/mysearch/main.go
├── internal/
│   ├── storage/   (db.go, models.go)
│   ├── indexer/   (walker, hasher, parser, tokenizer, builder, update)
│   ├── search/    (searcher.go)
│   └── dedup/     (deduper.go)
├── pkg/utils/     (ext.go)
├── go.mod
└── README.md
```

---