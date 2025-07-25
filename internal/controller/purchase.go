package controller

import (
	"encoding/gob"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/sirupsen/logrus"
	"github.com/topjohncian/cloudreve-pro-epay/internal/epay"
)

const (
	paymentTTL            = 3600 * 24 // 24h
	PurchaseSessionPrefix = "purchase_session_"
)

type PurchaseRequest struct {
	Name        string `json:"name" binding:"required"`
	OrderNo     string `json:"order_no" binding:"required"`
	NotifyUrl   string `json:"notify_url" binding:"required"`
	Amount      int    `json:"amount" binding:"required"`
	PaymentType string `json:"payment_type,omitempty"` // Added field to store payment type
}

type PurchaseResponse struct {
	Code int    `json:"code"`
	Data string `json:"data"`
}

func init() {
	gob.Register(&PurchaseRequest{})
}

func (pc *CloudrevePayController) Purchase(c *gin.Context) {
	// Get the payment type from the URL parameter
	paymentType := c.Param("type")
	if paymentType != "alipay" && paymentType != "wxpay" {
		logrus.WithField("type", paymentType).Debugln("无效的支付类型")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "无效的支付类型，仅支持 alipay 或 wxpay",
		})
		return
	}

	var req PurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logrus.WithError(err).Debugln("无法解析请求")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "无法解析请求" + err.Error(),
		})
		return
	}

	// Store the payment type in the request
	req.PaymentType = paymentType

	if err := pc.Cache.Set(PurchaseSessionPrefix+req.OrderNo, req, paymentTTL); err != nil {
		logrus.WithError(err).Warningln("无法保存订单信息")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"data":    "",
			"message": "无法保存订单信息" + err.Error(),
		})
		return
	}

	baseURL, _ := url.Parse(pc.Conf.Base)
	purchaseURL, err := url.Parse("/purchase/" + req.OrderNo)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"data":    "",
			"message": "无法解析 URL" + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, PurchaseResponse{
		Code: 0,
		Data: baseURL.ResolveReference(purchaseURL).String(),
	})
}

func (pc *CloudrevePayController) PurchasePage(c *gin.Context) {
	orderId := c.Param("id")
	if orderId == "" {
		logrus.Debugln("无效的订单号")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "无效的订单号",
		})
		return
	}

	req, ok := pc.Cache.Get(PurchaseSessionPrefix + orderId)
	if !ok {
		logrus.WithField("id", orderId).Debugln("订单信息不存在")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "订单信息不存在",
		})
		return
	}

	order, ok := req.(*PurchaseRequest)
	if !ok {
		logrus.WithField("id", orderId).Debugln("订单信息非法")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "订单信息非法",
		})
		return
	}

	// Validate payment type
	if order.PaymentType != "alipay" && order.PaymentType != "wxpay" {
		logrus.WithField("id", orderId).WithField("type", order.PaymentType).Debugln("无效的支付类型")
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"data":    "",
			"message": "无效的支付类型",
		})
		return
	}

	baseURL, _ := url.Parse(pc.Conf.Base)
	purchaseURL, _ := url.Parse("/notify/" + order.OrderNo)
	returnURL, err := url.Parse("/return/" + order.OrderNo)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"data":    "",
			"message": "无法解析 URL" + err.Error(),
		})
		return
	}

	amount := decimal.NewFromInt(int64(order.Amount)).Div(decimal.NewFromInt(100)).StringFixedBank(2)

	args := &epay.PurchaseArgs{
		Type:           epay.PurchaseType(order.PaymentType),
		ServiceTradeNo: order.OrderNo,
		Name:           order.Name,
		Money:          amount,
		Device:         epay.PC,
		NotifyUrl:      baseURL.ResolveReference(purchaseURL),
		ReturnUrl:      baseURL.ResolveReference(returnURL),
	}

	if pc.Conf.CustomName != "" {
		args.Name = pc.Conf.CustomName
	}

	client := epay.NewClient(&epay.Config{
		PartnerID: pc.Conf.EpayPartnerID,
		Key:       pc.Conf.EpayKey,
		Endpoint:  pc.Conf.EpayEndpoint,
	})

	endpoint, purchaseParams := client.Purchase(args)

	c.HTML(http.StatusOK, "purchase.tmpl", gin.H{
		"Endpoint": endpoint,
		"Params":   purchaseParams,
	})
}

func (pc *CloudrevePayController) QueryOrder(c *gin.Context) {
	orderId := c.Query("order_no")
	if orderId == "" {
		c.JSON(http.StatusOK, gin.H{
			"code":  http.StatusBadRequest,
			"error": "订单号不能为空",
		})
		return
	}

	_, ok := pc.Cache.Get(PurchaseSessionPrefix + orderId)

	// 如果在缓存中找不到订单，则认为已支付 (因为支付成功后会从缓存删除)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": "PAID",
		})
		return
	}

	// 否则，认为未支付
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": "UNPAID", // 或者文档中提到的 "其他值"
	})
}
