package main

import (
	"context"
	"drama-agent/internal/adapter/alipay"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/demopay"
	"drama-agent/internal/adapter/wechatpay"
	paymentapp "drama-agent/internal/application/payment"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/accounts"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type paymentResources interface {
	ports.PaymentStore
	ports.DemoPaymentStore
	CurrentUser(context.Context, string) (accounts.User, error)
}

// 构造器仅装配，不发商户请求。配置已由appconfig从私有文件验证并解析密钥。
func configuredPayments(cfg appconfig.Config, store paymentResources, runtime ports.RateLimiter) (*paymentapp.Service, error) {
	channels := map[string]ports.PaymentChannel{}
	if cfg.Payment.Demo.Enabled {
		client, err := demopay.New(store)
		if err != nil {
			return nil, fmt.Errorf("invalid configuration payment.demo")
		}
		channels[client.ID()] = client
	}
	if cfg.Payment.Wechatpay.Enabled {
		// map只用于按Serial验签；主动请求使用有序列表首项，不能随机选map键。
		if len(cfg.Payment.Wechatpay.VerifyKeys) == 0 {
			return nil, fmt.Errorf("invalid configuration payment.wechatpay.verifyKeys")
		}
		client, err := wechatpay.New(configuredWechatPayment(cfg.Payment))
		if err != nil {
			return nil, fmt.Errorf("invalid configuration payment.wechatpay")
		}
		channels[client.ID()] = client
	}
	if cfg.Payment.Alipay.Enabled {
		client, err := alipay.New(configuredAlipayPayment(cfg.Payment))
		if err != nil {
			return nil, fmt.Errorf("invalid configuration payment.alipay")
		}
		channels[client.ID()] = client
	}
	return &paymentapp.Service{Store: store, Channels: channels, Targets: paymentapp.PersonalTargets{}, Users: store, RateLimiter: runtime, Environment: cfg.Environment}, nil
}
func configuredWechatPayment(payment appconfig.Payment) wechatpay.Config {
	w := payment.Wechatpay
	return wechatpay.Config{
		MchID: w.MchID, AppID: w.AppID, MchSerialNo: w.MchSerialNo, PrivateKey: w.PrivateKey,
		APIV3Key: []byte(w.APIV3Key), VerifyKeys: w.PublicKeys, VerifySerial: w.VerifyKeys[0].ID,
		NotifyURL:      paymentNotifyURL(payment.NotifyBaseURL, "wechatpay"),
		RequestTimeout: appconfig.PaymentHTTPTimeout, SignatureWindow: appconfig.PaymentSignatureWindow,
		MaxResponseBytes: appconfig.PaymentMaxResponseBytes, NotifyMaxBytes: appconfig.PaymentNotifyMaxBytes,
		MaxBillCompressedBytes: appconfig.PaymentMaxBillCompressedBytes, MaxBillDecompressedBytes: appconfig.PaymentMaxBillDecompressedBytes,
		MaxBillCSVBytes: appconfig.PaymentMaxBillCSVBytes, MaxBillRows: appconfig.PaymentMaxBillRows, MaxBillFieldBytes: appconfig.PaymentMaxBillFieldBytes,
	}
}
func configuredAlipayPayment(payment appconfig.Payment) alipay.Config {
	a := payment.Alipay
	return alipay.Config{
		Gateway: a.Gateway, AppID: a.AppID, SellerID: a.SellerID, AppPrivateKeyFile: a.AppPrivateKeyFile, AlipayPublicKeyFile: a.AlipayPublicKeyFile,
		NotifyURL: paymentNotifyURL(payment.NotifyBaseURL, "alipay"), HTTPTimeout: appconfig.PaymentHTTPTimeout,
		ResponseMaxBytes: appconfig.PaymentMaxResponseBytes, BillCompressedMaxBytes: appconfig.PaymentMaxBillCompressedBytes,
		BillUncompressedMaxBytes: appconfig.PaymentMaxBillDecompressedBytes, BillCSVMaxBytes: appconfig.PaymentMaxBillCSVBytes,
		BillMaxEntries: appconfig.PaymentMaxBillEntries, BillMaxRows: appconfig.PaymentMaxBillRows, BillMaxFieldBytes: appconfig.PaymentMaxBillFieldBytes,
	}
}
func paymentNotifyURL(base, channel string) string {
	return strings.TrimRight(base, "/") + "/api/payments/notify/" + channel
}

type paymentMaintenance interface {
	ScanOrders(context.Context) error
	ScanRefunds(context.Context) error
	ReconcileBills(context.Context) error
}
type paymentJob struct {
	name              string
	interval, timeout time.Duration
	run               func(context.Context) error
}

func configuredPaymentJobs(service paymentMaintenance) []paymentJob {
	return []paymentJob{
		{name: "orders", interval: appconfig.PaymentScanInterval, timeout: appconfig.PaymentScanTimeout, run: service.ScanOrders},
		{name: "refunds", interval: appconfig.PaymentRefundInterval, timeout: appconfig.PaymentScanTimeout, run: service.ScanRefunds},
		{name: "bills", interval: appconfig.PaymentBillInterval, timeout: appconfig.PaymentBillTimeout, run: service.ReconcileBills},
	}
}
func startPaymentLoops(ctx context.Context, readOnly, hasChannels bool, service paymentMaintenance) <-chan struct{} {
	done := make(chan struct{})
	// 无渠道时只保留读取选项，不领取无法查单的旧订单；重新配置后原持久事实继续恢复。
	if readOnly || !hasChannels {
		close(done)
		return done
	}
	var wg sync.WaitGroup
	for _, job := range configuredPaymentJobs(service) {
		wg.Go(func() { runPaymentJob(ctx, job) })
	}
	go func() { wg.Wait(); close(done) }()
	return done
}
func runPaymentJob(ctx context.Context, job paymentJob) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		callCtx, cancel := context.WithTimeout(ctx, job.timeout)
		err := job.run(callCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			// 底层错误可能含token链接或协议正文。仅记录固定动作，详细安全事实由应用留运营审计。
			slog.ErrorContext(ctx, "payment_background_failed", "loop", job.name)
		}
		if ctx.Err() != nil {
			return
		}
		timer.Reset(job.interval)
	}
}
