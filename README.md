# go-license

A GUI-based license analysis tool for Go, Node.js, and Python projects

一个基于GUI的Go、Node.js和Python项目许可证分析工具

## Features 功能特性

- **Multi-language Support** 多语言支持：支持解析 Go 模块 (go.mod)、Node.js 项目 (package.json) 和 Python 项目 (pyproject.toml)
- **Multi-source Metadata** 多源元数据：从 pkg.go.dev v1 API、npm registry 和 PyPI 获取许可证信息
- **Rich Information** 丰富信息：提取许可证、作者、描述、版权、仓库链接等详细信息
- **Multi-file Selection** 多文件选择：可同时选择多个 manifest 文件，汇总成一份报告
- **CycloneDX SBOM** SBOM生成：将所有选中文件的直接依赖合并生成一份 CycloneDX 1.6 JSON SBOM（`sbom.json`，按 PURL 去重）
- **Excel Export** Excel导出：所有文件的依赖汇总成一份 `license_report.xlsx`，列结构统一，便于查看和管理
- **GUI Interface** 图形界面：使用文件选择对话框，用户友好
- **Progress Tracking** 进度跟踪：实时显示处理进度

## Usage 使用方法

1. Run the program:
```bash
go run main.go
```

2. 选择文件（可多选）：
   - 对于 Go 项目，选择 `go.mod` 文件
   - 对于 Node.js 项目，选择 `package.json` 文件
   - 对于 Python 项目，选择 `pyproject.toml` 文件

The tool will automatically detect each file's type and process accordingly.
工具会自动检测每个文件的类型并进行相应处理。所有文件的依赖会汇总成一份 Excel 报告
`license_report.xlsx` 和一份 `sbom.json`（CycloneDX 1.6，按 PURL 去重）。

## Output 输出内容

### Summary Excel report (license_report.xlsx):
所有选中文件的直接依赖汇总在一张表中（go.mod 中 `// indirect` 的依赖不包含），列结构统一：
- **Name** - 包名称
- **Version** - 版本
- **License** - 许可证类型
- **License URL** - 许可证URL
- **Author** - 作者
- **Description** - 描述
- **Copyright** - 版权信息
- **Repository** - 仓库地址
- **GitHub URL** - GitHub链接
- **Repository Type** - 仓库类型（go / npm / pypi）

### CycloneDX SBOM (sbom.json):
每次运行都会生成一份 `sbom.json`（CycloneDX 1.6 JSON），覆盖所有选中 manifest 文件的
直接依赖（范围符合 CRA Annex I Part II 的底线；go.mod 的 `// indirect` 依赖不包含，
其余生态的间接依赖需 lockfile 解析，列为后续增强）。
- **metadata.component** - 项目本体（type=application）
- **Components** - 每个直接依赖一个组件（type=library），含 Name、Version、PURL
  （`pkg:golang/...`、`pkg:npm/...`、`pkg:pypi/...`）、SPDX License ID + URL、Description、Author
- 跨文件重复依赖按 PURL 去重，`dependencies` 字段留空

### Internal packages 内部包过滤:
在公开 registry（pkg.go.dev / registry.npmjs.org / pypi.org）上查询不到（404）的依赖，
视为内部库或不可解析名称，**不会出现在 Excel 报告和 SBOM 中**。运行结束时提示被排除的数量。
网络错误、超时等不判定为内部库。

## Requirements 环境要求

- Go 1.25.0 or higher / Go 1.25.0 或更高版本

## Installation 安装步骤

```bash
git clone <repository-url>
cd go-license
go mod tidy
go run main.go
```

## Build Binary 构建可执行文件

```bash
go build -o go-license.exe main.go
```

## Dependencies 依赖库

- **[cyclonedx-go](https://github.com/CycloneDX/cyclonedx-go)** - CycloneDX SBOM generation / CycloneDX SBOM生成
- **[zenity](https://github.com/ncruces/zenity)** - Cross-platform GUI dialogs / 跨平台GUI对话框
- **[excelize](https://github.com/xuri/excelize/v2)** - Excel file operations / Excel文件操作
- **[golang.org/x/mod](https://golang.org/x/mod)** - Go module parsing / Go模块解析
- **[toml](https://github.com/BurntSushi/toml)** - TOML file parsing for Python projects / TOML文件解析（用于Python项目）

## Technical Details 技术细节

### Data Sources 数据源
- **Go modules**: pkg.go.dev v1 API (`/v1/module/` + `/v1/package/`)
- **Node.js packages**: https://registry.npmjs.org/
- **Python packages**: https://pypi.org/

### Error Handling 错误处理
- Network requests use context with 10-second timeout
- 网络请求使用带有10秒超时的上下文
- Graceful handling of missing metadata
- 优雅处理缺失的元数据
- User-friendly error messages with zenity dialogs
- 使用zenity对话框显示用户友好的错误消息

### License URL Generation 许可证URL生成
The tool generates SPDX license URLs: https://spdx.org/licenses/{SPDX_ID}
含空格或其他非法字符的 License 标识无法映射到稳定引用，对应 URL 留空。
工具使用以下格式生成许可证URL：https://spdx.org/licenses/{SPDX标识}

## Project Evolution 项目演进

该项目经过多次重构和优化：
- 改进了依赖信息获取逻辑
- 优化了错误处理和取消逻辑
- 支持多种项目类型（Go 和 npm）
- 提供更丰富的输出格式和字段
- 添加了图形用户界面支持

## Author 作者

License Tool / 许可证工具
