# license_fetcher

一个桌面 GUI 工具：解析 Go / Node.js / Python 项目的依赖清单，从公开 registry 抓取许可证等元数据，汇总生成 Excel 报告和 CycloneDX SBOM。

A GUI tool that parses dependency manifests of Go / Node.js / Python projects, fetches license metadata from public registries, and produces an Excel report plus a CycloneDX SBOM.

## 功能特性

- **多语言支持**：解析 `go.mod`、`package.json`、`pyproject.toml`（Poetry 与 PEP 621 两种风格）
- **多源元数据**：从 pkg.go.dev v1 API、npm registry 和 PyPI 查询许可证、作者、描述、版权、仓库链接
- **多文件汇总**：一次可选多个清单文件，合并成一份报告和一份 SBOM
- **CycloneDX SBOM**：1.6 JSON 格式，按 PURL 去重，覆盖 CRA Annex I Part II 要求的直接依赖范围
- **并发抓取**：8 个 worker 并发查询 registry，输出顺序仍与清单一致
- **全程 GUI**：文件选择、进度跟踪、结果提示均为图形对话框

## 使用方法

```bash
go run main.go   # 或 task run
```

在文件对话框中选择一个或多个清单文件（可跨项目混选），工具按文件名后缀自动识别类型：

| 文件 | 项目类型 | 解析范围 |
| --- | --- | --- |
| `go.mod` | Go | 直接依赖（`// indirect` 不包含） |
| `package.json` | Node.js | `dependencies` + `devDependencies` |
| `pyproject.toml` | Python | Poetry `dependencies` / `dev-dependencies`，或 PEP 621 `project.dependencies` |

运行结束后在当前工作目录生成两个文件：

- `sbom_report.xlsx` —— 所有选中文件的依赖汇总表
- `sbom.json` —— 合并后的 CycloneDX SBOM

## 输出说明

### Excel 汇总报告（sbom_report.xlsx）

所有选中文件的依赖合并在一张表中，列结构统一：

| 列 | 说明 |
| --- | --- |
| Name | 包名称 |
| Version | 版本（见下方「版本解析」） |
| License | 许可证类型 |
| License URL | SPDX 许可证链接 |
| Author | 作者 |
| Description | 描述 |
| Copyright | 版权信息 |
| Repository | 仓库地址 |
| GitHub URL | GitHub 链接 |
| Repository Type | 来源生态（go / npm / pypi） |

### CycloneDX SBOM（sbom.json）

- **metadata.component** —— 项目本体（type=application），名称由所有清单模块名以 `+` 连接
- **components** —— 每个直接依赖一个组件（type=library），含 Name、Version、PURL（`pkg:golang/…`、`pkg:npm/…`、`pkg:pypi/…`）、SPDX 许可证 ID + URL、描述、作者、版权
- 跨文件重复依赖按 PURL 去重，`dependencies` 字段留空
- 间接依赖需 lockfile 才能解析，列为后续增强

### 内部包过滤

在公开 registry 上查询返回 404 的依赖视为内部库或不可解析名称，不会出现在报告和 SBOM 中，运行结束时提示排除的数量。网络错误、超时不判定为内部包。

### 版本解析

清单中的非精确约束（`^1.2.3`、`~1.2.3`、`*`、`latest`、空）解析为 registry 实际提供的版本；精确 pin（`==x.y.z` 或裸版本号）保持不变。Go 模块采用 API 返回的实际版本。

### 许可证处理

- PyPI 许可证优先取 classifiers，并标准化为 SPDX ID（如 "MIT License" → `MIT`）
- 许可证 URL 统一为 `https://spdx.org/licenses/{SPDX_ID}.html`；无法映射到稳定引用的标识留空
- Go 模块优先从许可证文本中提取版权行作为 Copyright

## 构建与安装

环境要求：Go 1.25.0+

```bash
git clone https://github.com/jsfaint/license_fetcher.git
cd license_fetcher
```

开发运行：

```bash
task run    # 或 go run main.go
```

构建 Windows GUI 可执行文件（无控制台窗口）：

```bash
task build  # 产出 license.exe（GOOS=windows，CGO_ENABLED=0，-ldflags="-s -w -H=windowsgui"）
```

普通构建：

```bash
go build .
```

对话框基于 [zenity](https://github.com/ncruces/zenity)：Windows 无额外依赖，Linux 需要 GTK3，macOS 使用系统原生对话框。

## 技术细节

- **数据源**
  - Go 模块：pkg.go.dev v1 API（`/v1/module/` 取许可证与仓库，`/v1/package/` 取简介）
  - Node.js：`https://registry.npmjs.org/{name}/{version}`
  - Python：`https://pypi.org/pypi/{name}/json`
- **并发**：8 个 worker 的信号量池；进度更新加锁串行化；结果按索引回填，保证输出顺序稳定
- **超时**：每个 HTTP 请求独立 10s context；Go 模块端点冷缓存可能返回空 licenses，自动重试一次（404 视为内部包，不重试）
- **错误处理**：元数据缺失时保留该行并填充已获取字段；取消文件选择时静默退出

## 依赖库

- [cyclonedx-go](https://github.com/CycloneDX/cyclonedx-go) —— CycloneDX SBOM 生成
- [zenity](https://github.com/ncruces/zenity) —— 跨平台 GUI 对话框
- [excelize](https://github.com/xuri/excelize/v2) —— Excel 文件操作
- [golang.org/x/mod](https://pkg.go.dev/golang.org/x/mod) —— go.mod 解析
- [toml](https://github.com/BurntSushi/toml) —— TOML 解析（用于 pyproject.toml）

## License 许可证

[MIT](LICENSE)

## Author 作者

Jia Sui (jsfaint@gmail.com)
