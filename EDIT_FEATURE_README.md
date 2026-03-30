# 题目编辑功能完善说明

## 概述
完善了 `/edit?problem=id` 题目编辑界面，现在可以显示和修改题目的完整信息，并支持重新上传 tuack 压缩包。

## 功能特性

### 1. 显示题目完整信息
访问 `/edit?problem=id` 时会自动加载并显示：
- **题目 ID**（只读）
- **题目名称**（可编辑）
- **题目标题**（可编辑）
- **时间限制**（毫秒，可编辑）
- **内存限制**（MB，可编辑）
- **测试点数量**（只读）
- **题面 Markdown**（可编辑）
- **数据分组信息**（只读，显示每个分组的测试点编号和分数）

### 2. 修改并保存题目信息
提供两种保存方式：
- **保存题面**：仅保存 Markdown 题面内容
- **保存所有更改**：保存题目名称、标题、时间空间限制和题面内容

### 3. 重新上传 Tuack 压缩包
- 支持拖拽或点击上传 `.zip` 格式的 tuack 压缩包
- 自动解压并更新题目文件（题面、测试数据、数据分组等）
- 上传成功后自动刷新题目信息

## 技术实现

### 后端改动

#### 1. `web/problem.go`
- **扩展 `ProblemDataHandler`**：返回完整的题目信息，包括：
  - `id`, `name`, `title`, `description`
  - `time_limit_ms`, `mem_limit_mb`, `tests_count`
  - `statement` (Markdown 原文)
  - `statement_html` (渲染后的 HTML)
  - `config` (数据分组配置)

- **新增 `UpdateProblemHandler`**：处理题目信息更新
  - 权限检查（需要 `EditPermission`）
  - 更新数据库中的题目信息
  - 同步更新文件系统中的 `config.json` 和 `statement.md`

#### 2. `web/router.go`
- 新增路由：`POST /api/problem/{id}/update` - 更新题目元数据

#### 3. `tuack/import.go`
- **新增 `UpdateTuackPackage` 函数**：更新现有题目
  - 解压 tuack 压缩包
  - 清理旧的测试数据和 down 目录
  - 复制新的测试数据和 down 目录
  - 处理题面模板
  - 更新数据库中的题目信息

#### 4. `edit/edit.go`
- **修改 `ImportTuackHandler`**：调用 `UpdateTuackPackage` 更新现有题目而非创建新题目

### 前端改动

#### `static/edit.html`
完全重写了编辑界面：

1. **题目信息展示区**
   - 网格布局显示所有题目元数据
   - 可编辑字段：名称、标题、时间限制、内存限制
   - 只读字段：ID、测试点数量

2. **数据分组信息显示**
   - 从 `config.json` 读取分组信息
   - 显示每个分组的测试点编号和分数

3. **双标签页设计**
   - **编辑 Markdown 标签页**：编辑题面内容
   - **上传 Tuack 标签页**：上传压缩包

4. **智能加载**
   - 支持 URL 参数 `?problem=id` 自动加载
   - 加载按钮手动加载题目

5. **两种保存方式**
   - 保存题面（仅更新 Markdown）
   - 保存所有更改（更新所有元数据）

6. **Tuack 上传**
   - 支持拖拽上传
   - 实时显示处理进度
   - 上传成功后自动刷新

## 使用流程

### 方式一：修改题目信息
1. 访问 `/edit?problem=4`（或手动输入 ID 后点击加载）
2. 修改题目名称、标题、时间空间限制
3. 在 Markdown 编辑器中修改题面
4. 点击"保存所有更改"

### 方式二：重新上传 Tuack
1. 访问 `/edit?problem=4`
2. 切换到"上传 Tuack"标签页
3. 拖拽或选择 tuack 压缩包
4. 等待处理完成，系统会自动更新题目信息

## API 接口

### GET `/api/problem/{id}`
获取题目完整信息

**响应示例：**
```json
{
  "id": 4,
  "name": "problem_name",
  "title": "Problem Title",
  "description": "...",
  "time_limit_ms": 1000,
  "mem_limit_mb": 512,
  "tests_count": 20,
  "statement": "# Problem Statement\n...",
  "statement_html": "<h1>Problem Statement</h1>...",
  "config": {
    "test_cases": [
      {"cases": [1, 2, 3], "score": 10},
      {"cases": [4, 5, 6], "score": 20}
    ],
    "time_limit": 1000,
    "memory_limit": 512
  }
}
```

### POST `/api/problem/{id}/update`
更新题目元数据

**请求体：**
```json
{
  "name": "new_name",
  "title": "New Title",
  "description": "# New Statement\n...",
  "time_limit_ms": 2000,
  "mem_limit_mb": 256
}
```

**响应：**
```json
{
  "status": "success",
  "problem": {
    "id": 4,
    "name": "new_name",
    "title": "New Title",
    ...
  }
}
```

### POST `/api/import_tuack`
重新上传 tuack 压缩包

**请求体：** `multipart/form-data`
- `file`: zip 文件
- `problem_id`: 题目 ID

**响应：**
```json
{
  "status": "success",
  "problem_id": 4,
  "name": "problem_name",
  "title": "Problem Title",
  "message": "Problem updated successfully"
}
```

## 权限控制
所有修改操作都需要用户具有 `EditPermission` 权限，通过 `manage.CheckUserPermission` 进行验证。

## 数据一致性
- 数据库和文件系统同步更新
- `config.json` 存储时间/空间限制和数据分组
- `statement.md` 存储题面 Markdown
- 测试数据存储在 `tests/` 目录
- 样例数据存储在 `down/` 目录

## 测试建议
1. 访问 `/edit?problem=4` 测试加载功能
2. 修改题目名称、标题并保存，验证数据库和界面更新
3. 修改时间/空间限制并保存，验证 `config.json` 更新
4. 修改题面 Markdown 并保存，验证 `statement.md` 更新
5. 上传新的 tuack 压缩包，验证所有数据正确更新
6. 测试权限控制（使用无权限用户尝试修改）

## 备份
原始 `edit.html` 已备份为 `static/edit.html.bak`
