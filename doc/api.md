
## API 列表与输入输出格式
每个API均附带功能说明，输入输出字段均有详细含义和取值说明。


### /message [POST]
**功能**: 向公告板发送一条新消息。
**输入**:
	- message (string): 要发送的消息内容。
	- 支持JSON或表单格式。
**输出**:
	- 200 OK: 成功
	- 其他: 错误字符串


### /message/{index} [DELETE]
**功能**: 删除公告板上的一条消息。
**输入**:
	- index (int): 要删除的消息索引（从0开始）。
**输出**:
	- 200 OK: 成功
	- 其他: 错误字符串


### /board [GET]
**功能**: 获取公告板所有消息。
**输入**: 无
**输出**:
	- messages ([string]): 消息字符串数组。


### /submit [POST]
**功能**: 提交代码进行评测。
**输入**:
	- username (string): 用户名。
	- problem (string): 题目编号或名称。
	- code (string): 用户提交的源代码。
**输出**:
	- status (string): 状态，通常为"queued"（已入队）。
	- submission_id (int): 本次提交的唯一ID。


### /problem [GET]
**功能**: 获取题目详情页面。
**输入**: 无
**输出**: 题目详情HTML页面。


### /problemlist [GET]
**功能**: 获取题目列表页面。
**输入**: 无
**输出**: 题目列表HTML页面。


### /manage [GET]
**功能**: 获取管理界面页面。
**输入**: 无
**输出**: 管理界面HTML页面。


### /manage_users [GET]
**功能**: 获取用户管理页面。
**输入**: 无
**输出**: 用户管理HTML页面。


### /register [POST]
**功能**: 用户注册。
**输入**:
	- username (string): 用户名。
	- password (string): 密码。
	- group (string): 用户所属分组。
**输出**:
	- status (string): 注册结果（如"ok"、"fail"等）。
	- message (string): 详细提示信息。


### /login [POST]
**功能**: 用户登录。
**输入**:
	- username (string): 用户名。
	- password (string): 密码。
**输出**:
	- status (string): 登录结果（如"ok"、"fail"等）。
	- 其他: 用户信息、权限等。


### /last_submission [GET]
**功能**: 获取某用户对某题目的最近一次提交及其评测结果。
**输入**:
	- username (string): 用户名。
	- problem (string): 题目编号或名称。
**输出**:
	- submission (object): 最近一次提交对象，字段如下：
		- submission_id (int): 提交ID。
		- username (string): 用户名。
		- problem (string): 题目名。
		- code (string): 提交代码。
		- status (string): 状态（queued, running, accepted, wrong_answer, compile_error等）。
		- score (int): 得分。
		- time_ms (int): 用时（毫秒）。
		- memory_kb (int): 内存（KB）。
		- compileError (string): 编译错误信息（如有）。
	- results ([object]): 每个测试点的评测结果，字段如下：
		- test_index (int): 测试点编号。
		- passed (bool): 是否通过。
		- time_ms (int): 用时（毫秒）。
		- memory_kb (int): 内存（KB）。
		- output (string): 用户输出（如有）。


### /result/{user}/{problem} [GET]
**功能**: 获取某用户某题目的评测结果明细。
**输入**:
	- user (string): 用户名。
	- problem (string): 题目编号或名称。
**输出**: 评测结果内容或错误信息。


### /codefile/{user}/{problem} [GET]
**功能**: 获取某用户某题目的最后一次提交代码。
**输入**:
	- user (string): 用户名。
	- problem (string): 题目编号或名称。
**输出**: 代码内容或错误信息。


### /api/problem/{id} [GET]
**功能**: 获取题目的描述和配置信息。
**输入**:
	- id (string/int): 题目编号或名称。
**输出**:
	- statement.md (string): 题目描述（Markdown文本）。
	- config.json (object): 题目配置信息，字段如 time_limit, memory_limit, test_cases 等。


### /problems [GET]
**功能**: 获取题目列表（分页）。
**输入**:
	- page (int): 页码，从1开始。
	- per (int): 每页数量。
**输出**:
	- problems ([object]): 题目对象数组。
	- total (int): 题目总数。
	- page (int): 当前页码。
	- per (int): 每页数量。


### /api/users [GET]
**功能**: 获取用户列表。
**输入**: 无
**输出**:
	- users ([object]): 用户对象数组。
	- total (int): 用户总数。


### /api/allUsers [GET]
**功能**: 获取所有用户列表。
**输入**: 无
**输出**:
	- users ([object]): 用户对象数组。
	- total (int): 用户总数。


### /api/groups [GET]
**功能**: 获取所有分组信息。
**输入**: 无
**输出**:
	- groups ([object]): 分组对象数组。


### /api/user_permissions [GET]
**功能**: 获取当前用户的权限信息。
**输入**: 无
**输出**: 权限相关JSON对象。


### /api/pending_users [GET]
**功能**: 获取待审核用户列表。
**输入**: 无
**输出**:
	- users ([object]): 用户对象数组。
	- total (int): 用户总数。


### /api/approved_users [GET]
**功能**: 获取已审核通过的用户列表。
**输入**: 无
**输出**:
	- users ([object]): 用户对象数组。
	- total (int): 用户总数。


### /api/approve/{username} [POST]
**功能**: 审核通过指定用户。
**输入**:
	- username (string): 用户名。
**输出**:
	- status (string): "ok" 表示成功。


### /api/reject/{username} [POST]
**功能**: 拒绝指定用户的注册申请。
**输入**:
	- username (string): 用户名。
**输出**:
	- status (string): "ok" 表示成功。


### /api/create_user [POST]
**功能**: 管理员创建新用户。
**输入**: 用户信息JSON。
**输出**: 创建结果JSON。


### /api/create_group [POST]
**功能**: 管理员创建新分组。
**输入**: 分组信息JSON。
**输出**: 创建结果JSON。


### /api/update_group_creator [POST]
**功能**: 更新分组创建者。
**输入**: JSON。
**输出**: 结果JSON。


### /api/delete_group [POST]
**功能**: 删除分组。
**输入**: JSON。
**输出**: 结果JSON。


### /api/reset_password [POST]
**功能**: 重置用户密码。
**输入**: JSON。
**输出**: 结果JSON。


### /api/delete_user [POST]
**功能**: 删除用户。
**输入**: JSON。
**输出**: 结果JSON。


### /api/delete_problem [POST]
**功能**: 删除题目。
**输入**: JSON。
**输出**: 结果JSON。


### /edit/modify [POST]
**功能**: 修改题目信息。
**输入**: JSON。
**输出**: 结果JSON。


### /edit/add_test [POST]
**功能**: 添加测试点。
**输入**: JSON。
**输出**: 结果JSON。


### /api/import_tuack [POST]
**功能**: 导入tuack格式题目。
**输入**: multipart/form-data (zip文件)。
**输出**: 结果JSON。


### /api/upload_problem [POST]
**功能**: 上传新题目。
**输入**: multipart/form-data（题目信息及相关文件）。
**输出**: 结果JSON。


### /submissions [GET]
**功能**: 获取提交记录页面。
**输入**: 无
**输出**: 提交记录HTML页面。


### /submission/{id} [GET]
**功能**: 获取指定提交的详情页面。
**输入**:
	- id (int): 提交ID。
**输出**: 提交详情HTML页面。


### /api/submissions [GET]
**功能**: 获取提交记录列表（分页）。
**输入**:
	- page (int): 页码。
	- limit (int): 每页数量。
	- problem (string): 题目编号或名称（可选）。
	- username (string): 用户名（可选）。
**输出**:
	- total (int): 总记录数。
	- page (int): 当前页码。
	- limit (int): 每页数量。
	- submissions ([object]): 提交对象数组。


### /api/submission/{id} [GET]
**功能**: 获取指定提交的详细信息。
**输入**:
	- id (int): 提交ID。
**输出**: 提交详情JSON。


### /api/problem_stats [GET]
**功能**: 获取题目的统计信息。
**输入**:
	- problem (string): 题目编号或名称。
**输出**: 统计信息JSON。
