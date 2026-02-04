package bridge

func buildHelpPost() (title string, content [][]map[string]interface{}) {
	text := func(s string, styles ...string) map[string]interface{} {
		m := map[string]interface{}{
			"tag":  "text",
			"text": s,
		}
		if len(styles) > 0 {
			m["style"] = styles
		}
		return m
	}

	title = ""
	content = [][]map[string]interface{}{
		{text("可用命令：")},
		{text("1) "), text("/help 或 /h"), text(" —— 查看帮助")},
		{text("2) "), text("/pwd"), text(" —— 查看当前工作目录")},
		{text("3) "), text("/cd <绝对路径>"), text(" —— 切换工作目录")},
		{text("4) "), text("/status 或 /s"), text(" —— 查看当前状态")},
		{text("5) "), text("/queue 或 /q"), text(" —— 查看队列")},
		{text("6) "), text("/clear 或 /c"), text(" —— 清空当前会话上下文")},
		{text("7) "), text("/reset 或 /r"), text(" —— 重启 Codex")},
	}
	return title, content
}

func buildHelpFallbackText() string {
	return "可用命令：\n" +
		"/help 或 /h：查看帮助\n" +
		"/pwd：查看当前工作目录\n" +
		"/cd <绝对路径>：切换工作目录\n" +
		"/status 或 /s：查看当前状态\n" +
		"/queue 或 /q：查看队列\n" +
		"/clear 或 /c：清空当前会话上下文\n" +
		"/reset 或 /r：重启 Codex"
}
