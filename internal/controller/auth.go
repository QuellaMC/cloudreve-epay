package controller

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (pc *CloudrevePayController) BearerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := c.Request.Header.Get("Authorization")
		if authorization == "" || !strings.HasPrefix(authorization, "Bearer ") {
			logrus.WithField("Authorization", authorization).Debugln("Authorization 头缺失或无效")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"data":    "",
				"message": "Authorization 头缺失或无效",
			})
			return
		}

		var signature string
		if strings.HasPrefix(authorization, "Bearer Cr ") {
			signature = strings.TrimPrefix(authorization, "Bearer Cr ")
		} else {
			signature = strings.TrimPrefix(authorization, "Bearer ")
		}

		authorizations := strings.Split(signature, ":")
		if len(authorizations) != 2 {
			logrus.WithField("Authorization", authorization).WithField("len.auth", len(authorizations)).Debugln("Authorization 头无效")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"data":    "",
				"message": "Authorization 头无效",
			})
			return
		}

		// 验证是否过期
		expires, err := strconv.ParseInt(authorizations[1], 10, 64)
		if err != nil {
			logrus.WithField("Authorization", authorization).WithField("ttlUnix", authorizations[1]).Debugln("Authorization 头无效，无法解析 ttl")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"data":    "",
				"message": "Authorization 头无效，无法解析 ttl",
			})
			return
		}

		// 如果签名过期
		if expires < time.Now().Unix() && expires != 0 {
			logrus.WithField("Authorization", authorization).WithField("ttlUnix", authorizations[1]).Debugln("Authorization 头无效，签名已过期")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"data":    "",
				"message": "Authorization 头无效，签名已过期",
			})
			return
		}

		auth := &HMACAuth{
			CloudreveKey: []byte(pc.Conf.CloudreveKey),
		}

		if generatedSign := auth.Sign(getSignContent(c.Request), expires); signature != generatedSign {
			logrus.WithField("Authorization", authorization).WithField("generatedSign", generatedSign).Debugln("Authorization 头无效，签名不匹配")

			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"data":    "",
				"message": "Authorization 头无效，签名不匹配",
			})
			return
		}
	}
}

func (pc *CloudrevePayController) URLSignAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		signatureFromURL := c.Query("sign")
		if signatureFromURL == "" {
			logrus.Debugln("URL 签名参数 'sign' 缺失")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"message": "URL 签名参数 'sign' 缺失",
			})
			return
		}

		decodedSignatureFromURL, err := url.QueryUnescape(signatureFromURL)
		if err != nil {
			logrus.WithField("sign", signatureFromURL).Debugln("无法解码 URL 签名")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "无法解码 URL 签名"})
			return
		}

		parts := strings.Split(decodedSignatureFromURL, ":")
		if len(parts) != 2 {
			logrus.WithField("sign", decodedSignatureFromURL).Debugln("签名格式无效")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "签名格式无效"})
			return
		}

		expires, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			logrus.WithField("timestamp", parts[1]).Debugln("无法解析签名时间戳")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "无法解析签名时间戳"})
			return
		}

		if expires < time.Now().Unix() && expires != 0 {
			logrus.WithField("expires", expires).Debugln("签名已过期")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "签名已过期"})
			return
		}

		auth := &HMACAuth{
			CloudreveKey: []byte(pc.Conf.CloudreveKey),
		}

		// 根据 V4 文档，GET 请求仅对 URL Path 进行签名
		signContent := c.Request.URL.Path
		if signContent == "" {
			signContent = "/"
		}

		generatedSignature := auth.Sign(signContent, expires)

		if decodedSignatureFromURL != generatedSignature {
			logrus.WithField("received", decodedSignatureFromURL).WithField("generated", generatedSignature).WithField("path", signContent).Debugln("URL 签名不匹配")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    http.StatusUnauthorized,
				"message": "URL 签名不匹配",
			})
			return
		}

		c.Next()
	}
}
