package controller

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const PaymentMethodWechatNative = "wechat_native"

type WechatNativePayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

func getWechatPayMinTopup() int64 {
	if setting.WechatPayMinTopUp > 0 {
		if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
			dMin := decimal.NewFromInt(int64(setting.WechatPayMinTopUp))
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			return dMin.Mul(dQuotaPerUnit).IntPart()
		}
		return int64(setting.WechatPayMinTopUp)
	}
	return getMinTopup()
}

func loadWechatPayPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	if pk, err := utils.LoadPrivateKey(pemStr); err == nil {
		return pk, nil
	}
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM failed")
	}
	if block.Type == "RSA PRIVATE KEY" {
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if block.Type == "PRIVATE KEY" {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("not an RSA private key")
	}
	return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
}

func newWechatPayClient(ctx context.Context) (*core.Client, error) {
	if !setting.WechatPayNativeReady() {
		return nil, fmt.Errorf("微信支付未完整配置")
	}
	pk, err := loadWechatPayPrivateKey(setting.WechatPayMchPrivateKey)
	if err != nil {
		return nil, err
	}
	opts := []core.ClientOption{
		option.WithWechatPayAutoAuthCipher(
			setting.WechatPayMchId,
			setting.WechatPayMchCertSerial,
			pk,
			setting.WechatPayApiV3Key,
		),
	}
	return core.NewClient(ctx, opts...)
}

func wechatPayNotifyURL() string {
	base := service.GetCallbackAddress()
	u, err := url.Parse(base + "/api/wechatpay/notify")
	if err != nil {
		return base + "/api/wechatpay/notify"
	}
	return u.String()
}

// tryCompleteWechatNativeTopUp 根据微信返回的订单详情，将待支付 Native 订单置为成功并入账（幂等）。
// 若 content 非 SUCCESS，直接返回（无 reject、无 err）。
// clientReject 非空表示业务上不能接受该笔支付（回调应回复 FAIL）；err 表示落库失败（回调应 500 重试）。
// 调用方须已持有 LockOrder(topUp.TradeNo)。
func tryCompleteWechatNativeTopUp(topUp *model.TopUp, content *payments.Transaction) (clientReject string, err error) {
	if content == nil {
		return "missing transaction", nil
	}
	if content.TradeState == nil || *content.TradeState != "SUCCESS" {
		return "", nil
	}
	if content.OutTradeNo == nil || *content.OutTradeNo != topUp.TradeNo {
		return "out_trade_no mismatch", nil
	}
	if topUp.PaymentMethod != PaymentMethodWechatNative {
		return "bad payment method", nil
	}
	if content.Amount == nil || content.Amount.Total == nil {
		return "missing amount", nil
	}
	if content.Amount.Currency != nil && *content.Amount.Currency != "" && *content.Amount.Currency != "CNY" {
		return "invalid currency", nil
	}
	expectedFen := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if *content.Amount.Total != expectedFen {
		log.Printf("wechat topup amount mismatch trade=%s want=%d got=%d", topUp.TradeNo, expectedFen, *content.Amount.Total)
		return "amount mismatch", nil
	}
	if topUp.Status != common.TopUpStatusPending {
		return "", nil
	}
	topUp.Status = common.TopUpStatusSuccess
	topUp.CompleteTime = common.GetTimestamp()
	if err := topUp.Update(); err != nil {
		return "", fmt.Errorf("update order: %w", err)
	}
	dAmount := decimal.NewFromInt(int64(topUp.Amount))
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
	if err := model.IncreaseUserQuota(topUp.UserId, quotaToAdd, true); err != nil {
		return "", fmt.Errorf("quota: %w", err)
	}
	log.Printf("wechat native topup success %v", topUp)
	model.RecordLog(topUp.UserId, model.LogTypeTopup, fmt.Sprintf("使用微信 Native 充值成功，充值金额: %v，支付金额（元）: %f", logger.LogQuota(quotaToAdd), topUp.Money))
	return "", nil
}

// RequestWechatPayNative 微信 Native 下单，返回 code_url（人民币，与 getPayMoney 一致）
func RequestWechatPayNative(c *gin.Context) {
	if !setting.WechatPayNativeEnabled || !setting.WechatPayNativeReady() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "管理员未开启或未配置微信 Native 支付"})
		return
	}
	var req WechatNativePayRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.PaymentMethod != PaymentMethodWechatNative {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	minTop := getWechatPayMinTopup()
	if req.Amount < minTop {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTop)})
		return
	}
	if req.Amount > 100000 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值数量过大"})
		return
	}
	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	totalFen := decimal.NewFromFloat(payMoney).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	if totalFen < 1 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("USR%dNO%s", id, tradeNo)
	if len(tradeNo) > 32 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "订单号过长"})
		return
	}

	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(int64(amount))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}

	ctx := c.Request.Context()
	client, err := newWechatPayClient(ctx)
	if err != nil {
		log.Printf("wechat pay client: %v", err)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付客户端初始化失败"})
		return
	}

	svc := native.NativeApiService{Client: client}
	cny := "CNY"
	notifyStr := wechatPayNotifyURL()
	desc := fmt.Sprintf("充值TUC%d", amount)
	resp, _, err := svc.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(setting.WechatPayAppId),
		Mchid:       core.String(setting.WechatPayMchId),
		Description: core.String(desc),
		OutTradeNo:  core.String(tradeNo),
		NotifyUrl:   core.String(notifyStr),
		Amount: &native.Amount{
			Total:    core.Int64(totalFen),
			Currency: core.String(cny),
		},
	})
	if err != nil || resp == nil || resp.CodeUrl == nil || *resp.CodeUrl == "" {
		log.Printf("wechat native prepay failed: %v", err)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起微信支付失败"})
		return
	}

	topUp := &model.TopUp{
		UserId:        id,
		Amount:        amount,
		Money:         payMoney,
		TradeNo:       tradeNo,
		PaymentMethod: PaymentMethodWechatNative,
		CreateTime:    time.Now().Unix(),
		Status:        common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		log.Printf("wechat native create topup: %v", err)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"code_url": *resp.CodeUrl,
			"trade_no": tradeNo,
		},
	})
}

// GetWechatNativeTopUpOrder 查询当前用户微信 Native 充值订单状态（供前端轮询）。
// 若本地仍为 pending，主动向微信支付查单并对账，避免仅依赖异步通知导致长时间不到账。
func GetWechatNativeTopUpOrder(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	if tradeNo == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	userId := c.GetInt("id")
	topUp := model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != userId || topUp.PaymentMethod != PaymentMethodWechatNative {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单不存在"})
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": topUp.Status,
			},
		})
		return
	}
	if !setting.WechatPayNativeReady() {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": topUp.Status,
			},
		})
		return
	}

	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	topUp = model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil || topUp.UserId != userId || topUp.PaymentMethod != PaymentMethodWechatNative {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单不存在"})
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": topUp.Status,
			},
		})
		return
	}

	ctx := c.Request.Context()
	client, err := newWechatPayClient(ctx)
	if err != nil {
		log.Printf("wechat query order client trade=%s: %v", tradeNo, err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": topUp.Status,
			},
		})
		return
	}
	svc := native.NativeApiService{Client: client}
	txn, _, qErr := svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(tradeNo),
		Mchid:      core.String(setting.WechatPayMchId),
	})
	if qErr != nil {
		log.Printf("wechat query order trade=%s: %v", tradeNo, qErr)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"status": topUp.Status,
			},
		})
		return
	}

	reject, compErr := tryCompleteWechatNativeTopUp(topUp, txn)
	if reject != "" {
		log.Printf("wechat reconcile reject trade=%s: %s", tradeNo, reject)
	}
	if compErr != nil {
		log.Printf("wechat reconcile trade=%s: %v", tradeNo, compErr)
	}

	topUp = model.GetTopUpByTradeNo(tradeNo)
	if topUp == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"status": topUp.Status,
		},
	})
}

// WechatPayNotify 微信支付结果通知（APIv3）
func WechatPayNotify(c *gin.Context) {
	if !setting.WechatPayNativeReady() {
		writeWechatNotifyFail(c, "not configured")
		return
	}
	ctx := c.Request.Context()
	pk, err := loadWechatPayPrivateKey(setting.WechatPayMchPrivateKey)
	if err != nil {
		log.Printf("wechat notify load key: %v", err)
		writeWechatNotifyFail(c, "server error")
		return
	}

	mgr := downloader.MgrInstance()
	if !mgr.HasDownloader(ctx, setting.WechatPayMchId) {
		if err := mgr.RegisterDownloaderWithPrivateKey(
			ctx, pk, setting.WechatPayMchCertSerial, setting.WechatPayMchId, setting.WechatPayApiV3Key,
		); err != nil {
			log.Printf("wechat notify register downloader: %v", err)
			writeWechatNotifyFail(c, "server error")
			return
		}
	}
	verifier := verifiers.NewSHA256WithRSAVerifier(mgr.GetCertificateVisitor(setting.WechatPayMchId))
	handler, err := notify.NewRSANotifyHandler(setting.WechatPayApiV3Key, verifier)
	if err != nil {
		log.Printf("wechat notify handler: %v", err)
		writeWechatNotifyFail(c, "server error")
		return
	}

	content := new(payments.Transaction)
	_, err = handler.ParseNotifyRequest(ctx, c.Request, content)
	if err != nil {
		log.Printf("wechat notify parse: %v", err)
		writeWechatNotifyFail(c, "invalid notify")
		return
	}
	if content.OutTradeNo == nil || content.TradeState == nil {
		writeWechatNotifyFail(c, "missing fields")
		return
	}
	if *content.TradeState != "SUCCESS" {
		writeWechatNotifyOK(c)
		return
	}

	outNo := *content.OutTradeNo
	LockOrder(outNo)
	defer UnlockOrder(outNo)

	topUp := model.GetTopUpByTradeNo(outNo)
	if topUp == nil {
		log.Printf("wechat notify unknown order: %s", outNo)
		writeWechatNotifyFail(c, "order not found")
		return
	}

	reject, err := tryCompleteWechatNativeTopUp(topUp, content)
	if reject != "" {
		writeWechatNotifyFail(c, reject)
		return
	}
	if err != nil {
		log.Printf("wechat notify complete: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	writeWechatNotifyOK(c)
}

func writeWechatNotifyOK(c *gin.Context) {
	body, err := common.Marshal(gin.H{"code": "SUCCESS", "message": "成功"})
	if err != nil {
		_, _ = c.Writer.Write([]byte(`{"code":"SUCCESS","message":"成功"}`))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func writeWechatNotifyFail(c *gin.Context, msg string) {
	body, err := common.Marshal(gin.H{"code": "FAIL", "message": msg})
	if err != nil {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(`{"code":"FAIL","message":"error"}`))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}
