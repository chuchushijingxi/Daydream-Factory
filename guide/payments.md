# 支付与商户部署

本模板提供电脑网页微信 Native、支付宝当面付扫码和演示收银台。订单、到账、退款和对账共用同一应用流程；个人空间与团队均已接线：团队仅所有者可充值，到期或停用期间不显示该团队；团队订单退款以团队未分配余额为上限，不扣成员名下积分。

**真实渠道仅离线验证，未经真实商户联调。** 本仓库没有商户号、私钥或真实资金测试结果。部署者需要自行办理同一企业主体的渠道资质、开通对应支付与退款能力、准备正式协议正文，并完成自己的商户联调。第一期不包含H5、公众号、小程序、App支付、聚合支付或系统内开票。

## 默认部署与演示

`config/app.example.yaml` 的支付渠道全部关闭，环境缺省为 `production`。没有渠道也可正常启动，充值按钮隐藏，历史订单仍可查询。运营页面只管理固定档位及单日限额，不提供渠道开关或密钥管理。

在独立开发/测试资源中演示：

```yaml
environment: development # 也可 staging
payment:
  demo:
    enabled: true
```

保留原PG/Redis/S3/NSQ配置。不要把演示接入正式库或共用正式Redis命名空间。运营先配置并上架档位；用户选择固定档位、目标和渠道，同意协议后下单，再点击“模拟支付成功/失败”。演示无真实扣款，但写入真实订单与积分账本。未付单15分钟过期；关闭弹窗或换档位/渠道会先查后关。0.01元测试档位须勾选测试标识，仅非正式环境的运营可见。

正式环境开启demo会启动失败；即使支付宝未启用，`production` 搭配 `gateway: sandbox` 也会失败。不要绕过这些校验。

## 真实商户配置

把以下字段合并到已忽略的 `config/app.local.yaml`（权限0600），使用自己的配置值。示例占位符不是可用凭据。文件相对路径按YAML所在目录解析；推荐使用明确的只读挂载绝对路径。

```yaml
environment: production
payment:
  notifyBaseUrl: https://pay.example.invalid
  demo:
    enabled: false
  wechatpay:
    enabled: true
    mchId: "<商户号>"
    appId: "<已关联应用ID>"
    mchSerialNo: "<商户API证书序列号>"
    mchPrivateKeyFile: /run/secrets/wechatpay/apiclient_key.pem
    apiV3Key: "<32字节APIv3密钥>"
    verifyKeys:
      - id: "<微信支付公钥ID或平台证书序列号>"
        file: /run/secrets/wechatpay/wechatpay_public.pem
  alipay:
    enabled: true
    gateway: production
    appId: "<应用AppID>"
    sellerId: "<收款商家PID>"
    appPrivateKeyFile: /run/secrets/alipay/app_private_key.pem
    alipayPublicKeyFile: /run/secrets/alipay/alipay_public_key.pem
```

只启用其中一个渠道时将另一个 `enabled` 设false；关闭渠道不要求填写其商户字段。支付宝采用RSA2公钥模式，`sellerId` 为商家PID，与AppID不同；`alipayPublicKeyFile` 应是支付宝验签公钥。`gateway` 仅接受 `production|sandbox`，不是任意网关URL。沙箱仅非正式环境允许，须使用对应沙箱应用及密钥。

微信商户私钥和微信支付验签公钥是不同用途：私钥给请求加签，`verifyKeys` 负责应答和通知验签。支持1～8份不同ID的公钥或平台证书；第一项ID用于主动请求的 `Wechatpay-Serial`，服务收到的签名按返回Serial查对应key。不能把商户证书误填为平台验签文件。

RSA至少2048位，私钥接受PKCS#1或PKCS#8 PEM，公钥接受PUBLIC KEY、RSA PUBLIC KEY或CERTIFICATE PEM；每个文件仅一个PEM块、不超过64KiB。应用启动时读取并验证启用渠道配置，错误只输出字段名。不要把真实配置、密钥、证书或原始签名提交到仓库、工单、截图和日志。

轮换时先准备新文件、保留权限，更新配置后重启所有支付server。微信可保留新旧验签key并存，将当前主动使用ID置第一项，待在途通知/应答轮换完毕再移除旧key并重启；不会运行中自动下载平台证书或热加载文件。支付宝同样替换文件并重启。

## 容器挂载

默认Compose保留自己的 `runtime-config` 卷。将商户字段写入该部署实际使用的私有YAML，并通过自己的Compose override给应用添加只读挂载，例如：

```yaml
services:
  app:
    volumes:
      - /absolute/private/payment-keys:/run/secrets:ro
```

挂载中的文件路径须与YAML一致，宿主机权限须允许容器运行用户读取。不要重新生成或删除已有runtime-config、PG、Redis和对象存储卷来更新支付配置。备份商户文件需与既有配置/业务数据配套保管；本指南示例不会为你创建商户或暴露公网端口。

## 专用HTTPS回调入口

配置基址拼接为：

- `POST /api/payments/notify/wechatpay`
- `POST /api/payments/notify/alipay`

`notifyBaseUrl` 须为HTTPS，不能含用户名密码、query或fragment。该入口不走用户Cookie，但必须通过渠道验签、身份/金额/订单核对及到账事务确认；提交失败或结果不明返回失败，让渠道重发。请求体上限64KiB。只读服务返回503，不能用只读实例接收正式回调。

应用保留只允许回环/localhost Host的现有保护。部署者可以在单独的支付域名部署TLS反代，只公开上述两个精确POST路径，上游Host固定为localhost；不要放宽全站Host/Origin或把工作台、运营及所有API一并公开。宿主机Nginx片段示例（TLS证书配置由部署者提供）：

```nginx
# 位于部署者已有的 HTTPS server 块中；假设上游映射到本机8787。
location = /api/payments/notify/wechatpay {
    limit_except POST { deny all; }
    client_max_body_size 64k;
    proxy_set_header Host localhost:8787;
    proxy_pass http://127.0.0.1:8787;
}
location = /api/payments/notify/alipay {
    limit_except POST { deny all; }
    client_max_body_size 64k;
    proxy_set_header Host localhost:8787;
    proxy_pass http://127.0.0.1:8787;
}
location / { return 404; }
```

如基址带前缀，精确路径和反代重写须保持最终应用收到上述API路径。不要记录回调原始body、签名、私钥或账单下载token；只保留应用安全事件及告警。回调成功回执由应用产生，代理不能把上游503改为200。检查真实DNS、TLS、网络可达及渠道后台通知设置后，再由部署者开展自己的商户联调。

## 运营与验收边界

档位名称仅供运营辨认，用户充值页面继续展示金额。运营可配置原价和可选优惠价，并独立设置充值积分与赠送积分，优惠和赠分可叠加；优惠价留空时按原价付款，有优惠时用户能看到实付与划线原价。固定档位冻结原价/实付/充值/赠送快照，后续改价不影响旧单。默认每人北京时间单日成功支付上限¥5,000，运营可配置0为不限；每人同时最多1个待付单，每分钟最多5次新建。运营退款按订单实付金额及未使用充值积分比例报价，按分向下取整，不按原价退款；只可下调、必填原因并二次确认；积分先按原分类扣回，结果未知按原退款号核对，不能换号重发。充值记录提供线下开票提示，系统不保存开票信息；协议当前是明确标示的研发占位稿。

渠道适配器使用标准库完成签名/验签，不包含支付SDK。微信应答/通知需RSA验签、APIv3通知需AES-GCM解密；支付宝对原始应答字节及通知参数验签。下载账单受官方域名、重定向、压缩/解压/字段数量限制。离线测试仅使用测试内生成的密钥和自造报文；工程测试通过不等于真实商户支付、退款、对账或法务条款验收。
