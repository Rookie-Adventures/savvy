# 中文 I18n 完成计划

## 目标
完成 hermes-workspace 前端所有英文字符串的中文翻译。

## 现状
- `src/lib/i18n.ts` 已有基础翻译框架，支持 11 种语言
- 中文 (zh) 已翻译 40 个 key（nav/skills/profiles/tasks/jobs/settings/common）
- 大量 UI 字符串仍为硬编码英文

## ⚠️ 翻译边界规则（重要）

### ✅ 应该翻译的（用户可见 UI 文本）
- Toast 消息（如 "Task created"）
- 页面标题/标题（如 "Agents", "Settings"）
- 按钮标签（如 "Save", "Cancel", "Delete"）
- 占位符文本（如 "Search..."）
- 描述说明（如 "Choose the display language"）
- 空状态提示（如 "No data"）
- 加载提示（如 "Loading..."）
- 面向用户的错误提示
- 确认对话框文本

### ❌ 不应该翻译的（技术标识符，翻译会破坏功能）
- **路由路径**：`/settings`、`/chat`、`/tasks`、`/memory`
- **组件 key/ID**：`'dashboard'`、`'sidebar'`、`'chat-panel'`
- **状态枚举值**：`status: 'active'`、`'pending'`、`'backlog'`、`'review'`、`'done'`、`'blocked'`
- **斜杠命令**：`/connect`、`/voice`、`/dream`、`/distill`、`/goal`
- **环境变量名**：`EDITOR`、`NODE_ENV`、`API_KEY`
- **API 端点**：`/api/sessions`、`/api/skills`
- **CSS 类名/变量名**
- **数据库字段名**
- **日志/调试文本**
- **测试断言文本**

## 待翻译内容

### 1. Toast 消息 (~21 条)
```
Agent config saved → 代理配置已保存
Are you sure? → 确定吗？
Chat cleared → 聊天已清空
Cleared → 已清空
Coming soon → 即将推出
Conversation exported → 对话已导出
Failed to send message → 发送消息失败
Job created → 作业已创建
Job deleted → 作业已删除
Job paused → 作业已暂停
Job resumed → 作业已恢复
Job triggered → 作业已触发
Job updated → 作业已更新
Model config saved — takes effect on next message → 模型配置已保存 — 下条消息生效
Network error — could not remove provider. → 网络错误 — 无法移除提供者
No control key available for this agent → 此代理无可用控制键
Saved → 已保存
Task created → 任务已创建
Task deleted → 任务已删除
Task updated → 任务已更新
```

### 2. 页面标题 headings (~15+ 个)
```
Agents → 代理
Operations → 操作
Echo Studio → 回声工作室
Recent Missions → 最近任务
New Mission → 新建任务
Conductor settings → 调度器设置
Choose project directory → 选择项目目录
Continue Mission → 继续任务
Task Output → 任务输出
Create Job → 创建作业
Edit Job → 编辑作业
Schedule → 调度
Options → 选项
```

### 3. 其他 UI 字符串
- 按钮标签
- 表单占位符
- 空状态提示
- 错误消息
- 确认对话框文本

## 执行步骤

### T1.1 - 收集所有缺失的翻译 key
用 CodeGraph + grep 扫描所有 .tsx 文件，提取：
- `toast('...')` 中的消息
- `<h1/h2/h3>` 标签中的标题
- `placeholder="..."` 中的占位符
- `label="..."` 中的标签
- 确认对话框文本

### T1.2 - 补充 i18n.ts 翻译
将收集到的字符串添加到 EN 和 ZH 对象中。

### T1.3 - 替换 screens/ 中的硬编码
逐文件替换，确保每个 t() 调用使用正确的 key。

### T1.4 - 替换 components/ 中的硬编码
同上。

### T1.5 - 验证
- 运行 typecheck 确保无类型错误
- 运行 lint 确保代码规范
- 检查是否有遗漏的硬编码字符串
