# feishu-codex-bridge

把飞书消息转发到本机 Codex（ACP 模式）的桥接服务。

## 构建二进制

在仓库根目录执行：

```bash
go build -o ./bin/feishu-codex-bridge .
```

也可以直接输出到你希望的安装位置（可能需要权限）：

```bash
go build -o /usr/local/bin/feishu-codex-bridge .
```

或者安装到你的 Go bin（由 `GOBIN`/`GOPATH` 决定）：

```bash
go install .
```

## 运行

需要配置环境变量（可写在 `.env`）：
- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- 可选：`CODEX_MODEL`（默认值在模板里，首次生成通常为 `gpt-5.2-codex`）、`SESSION_DB_PATH`、`SESSION_IDLE_MINUTES`、`SESSION_RESET_HOUR`

### 默认配置目录（推荐）

为了让你可以在任意目录执行二进制，本项目默认使用：

- 配置目录：`~/.feishu-codex-bridge/`
- 默认环境变量文件：`~/.feishu-codex-bridge/.env`

首次运行如果该文件不存在，会自动生成一份模板，请你编辑填入 `FEISHU_APP_ID/FEISHU_APP_SECRET`。
如果必填项未配置，程序会直接退出（exit code 2），并且不会启动 Codex。

补充规则：
- 先加载 `~/.feishu-codex-bridge/.env`（不会覆盖已存在的系统环境变量）
- 可选：如果存在 `<workdir>/.feishu-codex-bridge/.env`，会用它覆盖全局默认（避免和项目自身 `.env` 冲突）

### 工作目录参数（你选的 A 规则）

`--workdir` > `WORKING_DIR` > 默认 `.`

示例：

```bash
./bin/feishu-codex-bridge --workdir /path/to/your/project
```

## 飞书内命令

在飞书群/私聊里可以发送：

- `/help`：查看命令帮助
- `/pwd`：查看当前工作目录
- `/cd /absolute/path` 或 `/workdir /absolute/path`：切换工作目录（bridge 不重启，会重启 codex app-server；会清掉当前 chat 的会话线程）
- `/clear`：清空当前 chat 的会话上下文（不切换目录、不重启 bridge/codex，只是从头开始）

## 回复引用

本程序会优先以“回复消息（引用原消息）”的方式进行输出：每条回复都会引用触发它的那条用户消息，避免多人/多条消息时串行错乱。

## 撤回消息

如果用户在飞书里撤回了一条已发送的消息：
- 若该消息仍在队列中未处理：会被自动跳过，不会再触发回复
- 若该消息正在处理中：会中断当前处理并清空该 chat 的会话上下文，避免继续回复被撤回的内容

## 单实例运行

本程序默认使用文件锁确保单实例运行：`~/.feishu-codex-bridge/bridge.lock`。
如果你看到“another instance is running (pid xxx)”或启动时提示 `PID=xxx`，请手动结束该进程后再启动，例如：

```bash
kill -TERM xxx
# 若仍未退出：
kill -KILL xxx
```
