// Package display provides Chinese labels without changing machine-readable identifiers.
package display

var statuses = map[string]string{
	"duplicate_host": "地址重复", "remote_filename_collision": "远端文件名冲突", "partial_applied": "部分执行完成", "persisted_with_error": "已保存但存在错误", "manual_required": "需要人工处理", "matches": "一致", "succeeded": "成功", "no_devices": "没有设备", "backed_up": "备份成功", "failed": "失败", "filtered": "已筛除", "sample_limit": "抽样跳过", "saved_with_error": "已保存但存在错误", "reported_uploaded": "设备报告已上传", "reported_with_error": "设备报告完成但存在错误", "interrupted": "已中断", "not_started": "未开始", "complete": "完成", "completed": "完成", "incomplete": "未完成", "success": "成功", "pending": "待处理", "ready": "就绪", "planned": "计划已生成", "partial_planned": "计划部分生成", "discovered": "发现完成", "applied": "执行完成", "unchanged": "无需变更", "partial": "部分完成", "missing_credentials": "缺少凭据", "unsupported_operation": "不支持此操作", "duplicate": "地址重复", "unmapped": "厂商未映射", "unsupported": "不支持", "change": "待变更", "missing": "缺失", "matched": "一致", "mismatch": "不一致", "plan": "规划", "discover": "发现", "apply": "执行", "unknown": "未知", "content_mismatch": "内容不一致", "missing_description": "缺少描述", "unverified": "未核验", "unsupported_vendor": "不支持的厂商", "invalid_address": "地址无效", "duplicate_address": "地址重复", "created": "新建", "changed": "已变更",
}
var errors = map[string]string{
	"login_timeout": "登录超时，请检查 SSH 握手及登录对话", "authentication_error": "认证失败，请检查现有凭据", "authentication_failed": "认证失败", "connection_closed": "连接已关闭", "connection_timeout": "连接超时", "connection_refused": "连接被拒绝", "connection_reset": "连接被重置", "connection_error": "连接失败", "connection_failed": "连接失败", "no_route": "目标无可用路由", "command_timeout": "命令超时", "write_timeout": "发送超时", "prompt_error": "未匹配到预期提示符", "invalid_output_encoding": "输出编码无效或无法确认", "incomplete_configuration": "配置采集不完整", "uncommitted_configuration": "存在未提交配置", "unsupported_configuration_format": "配置格式不受支持", "device_error": "设备拒绝命令", "configuration_error": "配置采集失败", "transport_error": "传输失败", "output_limit_exceeded": "输出超过上限", "script_output_limit_exceeded": "脚本累计输出超过上限", "host_key_changed": "主机密钥发生变化", "host_key_untrusted": "主机密钥不受信任", "session_busy": "设备会话正忙", "backup_busy": "备份路径正忙", "log_error": "会话日志写入失败", "parsing_error": "解析失败", "parse_failed": "解析失败", "file_write_failed": "文件写入失败", "file_persistence_unconfirmed": "文件持久化未确认", "backup_persistence_unconfirmed": "备份持久化未确认", "backup_finalize_failed": "备份收尾失败", "backup_durability_unsupported": "文件系统不支持持久化确认", "internal_error": "内部操作失败", "persistence_unconfirmed": "持久化未确认", "missing_credentials": "缺少凭据", "cache_namespace_required": "API key 模式启用清单缓存时，请设置非敏感账户标识 ND_USERNAME 或 ND_CACHE_NAMESPACE", "evidence_limit_exceeded": "设备回显过多，无法安全确认上传结果", "transfer_unconfirmed": "上传结果未确认", "transfer_failed": "上传失败", "unsupported_operation": "不支持此操作",
}

func Status(code string) string {
	if text, ok := statuses[code]; ok {
		return text
	}
	return "未知状态"
}

// Error explains only known codes; raw error strings never enter user output.
func Error(code string) string {
	if text, ok := errors[code]; ok {
		return text + "（" + code + "）"
	}
	if code == "" {
		return "无"
	}
	return "操作未完成"
}
func Bool(value bool) string {
	if value {
		return "是"
	}
	return "否"
}
func Policy(code string) string {
	switch code {
	case "strict":
		return "严格"
	case "selected":
		return "所选设备"
	case "verified":
		return "文件已核验"
	}
	return "未知策略"
}

func SourceState(committed, verified bool) string {
	if verified {
		return "原文已保留"
	}
	if committed {
		return "原文已提交，持久化未确认"
	}
	return "原文保存未确认"
}
