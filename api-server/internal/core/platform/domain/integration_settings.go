package domain

// Connection setting keys every platform Integration may carry; a Platform module defines its own
// keys beside them. A user sets them on an Integration they register, and swallow sets them on
// the Integration it records for a Platform it deployed; the platform API clients read them when
// they connect. The keys are stored on Integrations, so renaming one silently drops the setting
// from every existing Integration.
const (
	SettingTimeout            = "timeout"
	SettingInsecureSkipVerify = "insecureSkipVerify"
	// SettingCACertificate is the PEM CA the Platform API's certificate is verified against. A
	// deployment that returns its cluster CA records it instead of insecureSkipVerify.
	SettingCACertificate = "caCertificate"
)
