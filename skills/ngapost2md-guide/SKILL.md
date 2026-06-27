---
name: ngapost2md-guide
description: 使用 PATH 中的 ngapost2md 抓取 NGA 帖子为 Markdown，并生成阅读友好的论坛观点分析。用户要求下载、总结、分析论坛观点、处理 NGA 链接/tid、生成 HTML 报告、梳理争议和共识时使用此技能。
---

# 论坛观点分析

用于把 NGA 帖子抓取成 Markdown，并分析楼里不同观点、争议和共识。默认面向阅读友好输出：短句、先结论、少层级。

## 抓取

默认使用临时目录，避免把一次性帖子产物留在项目工作区：

```bash
tmpdir=$(mktemp -d)
cd "$tmpdir"
ngapost2md 'https://bbs.nga.cn/read.php?tid=123456'
```

用户给完整链接时，优先直接传完整链接；只看某用户时加 `--authorid <uid>`。

除非用户明确要求保存/归档，否则不要在当前仓库或项目目录长期保留下载结果。

## 认证

`ngapost2md` 可使用 `config.ini` 中的 NGA Cookie，也可在未配置时读取本机 Chrome Cookie。运行时不要输出、记录或泄露 Cookie、UID、CID 等认证信息。

如果需要生成配置文件，先确认当前目录没有重要 `config.ini`，避免覆盖：

```bash
ngapost2md --gen-config-file
```

## 长帖

如果用户说明是长帖、热帖、多页讨论，优先在临时目录开启切分：

```bash
ngapost2md --gen-config-file
# 修改 config.ini 中 [post] split_md_file，例如 5 或 10
ngapost2md 'https://bbs.nga.cn/read.php?tid=123456'
```

`split_md_file = N` 表示每个 Markdown 约 N 页，1 页约 20 楼。切分后通常是 `post-001.md`、`post-002.md`。

分析阶段按规模处理：

- 小于约 120KB：直接全量分析。
- 120KB-500KB 或 2-5 个切分文件：分段笔记后汇总。
- 超过约 500KB 或 6 个以上切分文件：可用 subagent/子会话分工。
- 单文件超过约 1MB、总量超过约 2MB、或切分文件超过约 10 个：先问用户确认全量、抽样、只看高赞/前 N 页，还是分批处理。

## 分析

只基于本地生成的 Markdown，不联网重新查帖子。

默认输出短版，约 300-600 字：

```text
**一句话结论**
...

**观点地图**
- [多数] ...
- [不少] ...
- [少数] ...
- [补充] ...

**3 个争议**
1. ...
2. ...
3. ...

**我的判断**
...
```

判断频率时用自然语言和热度等级，不要把个别楼层写成共识。

最后可以给 Agent 自己的判断，但要单独标出“我的判断”。先总结楼里观点，再输出自己的判断；不要把自己的判断写成论坛共识。判断应基于帖子内容，简短、克制，说明依据和不确定性。

## HTML 报告

用户要求“好看一点”“适合阅读障碍”“HTML”“报告”“可视化”时，生成临时 HTML 报告。不要手写完整 HTML；只生成 JSON 数据，再用脚本渲染。

使用本 skill 自带文件：

- 模板：`references/forum-opinion-report-template.html`
- 渲染脚本：`scripts/render_forum_report.py`

流程：

1. 生成 `report-data.json`。
2. 运行 `python3 <skill>/scripts/render_forum_report.py report-data.json -o report.html --open`。
3. 最终回复只给路径和是否已打开，不要贴长篇分析。

JSON 结构：

```json
{
  "title": "报告标题",
  "tid": "123456",
  "threadUrl": "https://bbs.nga.cn/read.php?tid=123456",
  "floorRange": "0-200 楼",
  "analyzedAt": "2026-06-27 14:30",
  "tldr": "一句话结论，35-50 个中文字符以内更好",
  "summaryCards": [{"title": "主流观点", "body": "..."}],
  "opinionMap": [{"role": "楼主", "title": "...", "body": "..."}],
  "disputes": [{"title": "...", "sides": [{"label": "一方", "body": "..."}, {"label": "另一方", "body": "..."}]}],
  "heatItems": [{"label": "话题", "level": 4, "note": "多数反复出现"}],
  "evidence": [{"title": "依据", "body": "最多 2 句"}],
  "agentTake": "明确不是楼内共识，最多 2-3 句",
  "sourcePath": "/path/to/post.md"
}
```

`analyzedAt` 可省略，脚本会用当前时间。`threadUrl` 可省略，脚本会根据 `tid` 生成 NGA 链接。

`heatItems.level`：4=多数，3=不少，2=少数，1=个别。脚本会渲染成热度条和等级文字。

HTML 风格要求：

- 不使用大 hero；第一屏紧凑，10 秒内读懂。
- 正文 16-18px，行距 1.65-1.8，宽度约 760-860px。
- 卡片最多 2 句，短句优先。
- 不使用 `<details>` 折叠；详情也要压缩成可扫读的依据卡片。
- 不使用看似精确的次数，优先使用热度条和“多数/不少/少数/个别”。
- 不依赖外部 CSS、JS、CDN 或用户本机其它仓库。

## 不要做

- 不修改源码，除非用户明确要求开发工具。
- 不提交代码。
- 不泄露认证信息。
- 不把一次性下载产物留在项目工作区。
