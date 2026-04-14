package setting

// WechatPayNativeEnabled 为 true 且其余直连参数齐全时，启用微信 Native 充值
var WechatPayNativeEnabled = false

// 直连商户号、公众号/小程序等绑定的 AppID（以商户平台为准）
var WechatPayMchId = ""
var WechatPayAppId = ""

// WechatPayApiV3Key 商户 APIv3 密钥（32 字节）；选项名以 Key 结尾，GetOptions 不向客户端返回
var WechatPayApiV3Key = ""

// WechatPayMchCertSerial 商户 API 证书序列号
var WechatPayMchCertSerial = ""

// WechatPayMchPrivateKey 商户 API 私钥 PEM（PKCS#8 或 PKCS#1）；选项名以 Key 结尾，GetOptions 不向客户端返回
var WechatPayMchPrivateKey = ""

// WechatPayMinTopUp 为 0 时使用运营全局 MinTopUp（与 getMinTopup 一致）
var WechatPayMinTopUp = 0

// WechatPayNativeReady 判断是否已配置直连 Native 支付（不含 Enabled 开关）
func WechatPayNativeReady() bool {
	if WechatPayMchId == "" || WechatPayAppId == "" || WechatPayApiV3Key == "" ||
		len(WechatPayApiV3Key) != 32 || WechatPayMchCertSerial == "" || WechatPayMchPrivateKey == "" {
		return false
	}
	return true
}
