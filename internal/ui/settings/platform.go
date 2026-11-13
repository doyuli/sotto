package settings

func credentialProtectionCopy(goos string) (label, hint string) {
	switch goos {
	case "darwin":
		return "macOS 钥匙串保护", "凭据保存在 macOS 钥匙串中，不写入设置文件"
	case "windows":
		return "Windows 凭据保护", "凭据由 Windows Credential Manager 本地加密保护"
	default:
		return "本地凭据存储", "凭据单独存储，不写入设置文件"
	}
}
