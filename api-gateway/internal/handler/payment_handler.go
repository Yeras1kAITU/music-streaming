package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	paymentpb "github.com/music-streaming/proto/payment"
)

type PaymentHandler struct {
	paymentClient paymentpb.PaymentServiceClient
}

func NewPaymentHandler(client paymentpb.PaymentServiceClient) *PaymentHandler {
	return &PaymentHandler{paymentClient: client}
}

func (h *PaymentHandler) CreateSubscription(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req paymentpb.CreateSubscriptionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = userID.(string)

	resp, err := h.paymentClient.CreateSubscription(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) CancelSubscription(c *gin.Context) {
	userID, _ := c.Get("user_id")
	subscriptionID := c.Param("id")

	resp, err := h.paymentClient.CancelSubscription(c.Request.Context(), &paymentpb.CancelSubscriptionRequest{
		SubscriptionId: subscriptionID,
		UserId:         userID.(string),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) GetSubscription(c *gin.Context) {
	userID, _ := c.Get("user_id")

	resp, err := h.paymentClient.GetSubscription(c.Request.Context(), &paymentpb.GetSubscriptionRequest{UserId: userID.(string)})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription not found"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) ProcessPayment(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req paymentpb.ProcessPaymentRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = userID.(string)

	resp, err := h.paymentClient.ProcessPayment(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) GetPaymentHistory(c *gin.Context) {
	userID, _ := c.Get("user_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	resp, err := h.paymentClient.GetPaymentHistory(c.Request.Context(), &paymentpb.GetPaymentHistoryRequest{
		UserId:   userID.(string),
		Page:     int32(page),
		PageSize: int32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) ApplyCoupon(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req paymentpb.ApplyCouponRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.UserId = userID.(string)

	resp, err := h.paymentClient.ApplyCoupon(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *PaymentHandler) GetPricingPlans(c *gin.Context) {
	resp, err := h.paymentClient.GetPricingPlans(c.Request.Context(), &paymentpb.GetPricingPlansRequest{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
