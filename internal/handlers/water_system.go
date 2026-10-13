package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cseptian/rukun-warga-api/internal/database"
	"github.com/cseptian/rukun-warga-api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CalculateTotalCost calculates water bill based on tiered usage
func CalculateTotalCost(volume float64) float64 {
	cost := 0.0
	if volume <= 10 {
		cost = volume * 1.00
	} else if volume <= 20 {
		cost = 10*1.00 + (volume-10)*1.50
	} else {
		cost = 10*1.00 + 10*1.50 + (volume-20)*2.00
	}
	return cost
}

func CreateNeighbor(c *gin.Context) {
	var input struct {
		Name    string `json:"name" binding:"required"`
		Address string `json:"address" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Address) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and address must be non-empty"})
		return
	}

	neighbor := models.Neighbor{
		Name:     strings.TrimSpace(input.Name),
		Address:  strings.TrimSpace(input.Address),
		JoinDate: time.Now(),
	}

	if err := database.DB.Create(&neighbor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create neighbor"})
		return
	}

	c.JSON(http.StatusCreated, neighbor)
}

func ListNeighbors(c *gin.Context) {
	var neighbors []models.Neighbor
	if err := database.DB.Find(&neighbors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch neighbors"})
		return
	}
	c.JSON(http.StatusOK, neighbors)
}

func GetNeighbor(c *gin.Context) {
	id := c.Param("id")
	var neighbor models.Neighbor
	if err := database.DB.Preload("Readings").First(&neighbor, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Neighbor not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch neighbor"})
		}
		return
	}
	c.JSON(http.StatusOK, neighbor)
}

func CreateReading(c *gin.Context) {
	var input struct {
		NeighborID string   `json:"neighbor_id" binding:"required"`
		Volume     *float64 `json:"volume"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Volume == nil || *input.Volume <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Volume must be strictly greater than zero"})
		return
	}

	// Verify neighbor exists
	var neighbor models.Neighbor
	if err := database.DB.First(&neighbor, "id = ?", input.NeighborID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Neighbor not found"})
		return
	}

	cost := CalculateTotalCost(*input.Volume)

	reading := models.Reading{
		NeighborID: input.NeighborID,
		Date:       time.Now(),
		Volume:     *input.Volume,
		TotalCost:  cost,
		IsPaid:     false,
	}

	// Use a transaction
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&reading).Error; err != nil {
			return err
		}

		// Update neighbor
		now := time.Now()
		neighbor.LastReadingDate = &now
		neighbor.LastReadingVolume = *input.Volume
		neighbor.TotalUnpaid += cost

		if err := tx.Save(&neighbor).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create reading"})
		return
	}

	c.JSON(http.StatusCreated, reading)
}

func TogglePaymentStatus(c *gin.Context) {
	id := c.Param("id")

	var reading models.Reading
	if err := database.DB.First(&reading, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Reading not found"})
		return
	}

	// Update the payment status and adjust the neighbor's TotalUnpaid
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var neighbor models.Neighbor
		if err := tx.First(&neighbor, "id = ?", reading.NeighborID).Error; err != nil {
			return err
		}

		if reading.IsPaid {
			// Unpaying
			reading.IsPaid = false
			neighbor.TotalUnpaid += reading.TotalCost
		} else {
			// Paying
			reading.IsPaid = true
			neighbor.TotalUnpaid -= reading.TotalCost
		}

		if err := tx.Save(&reading).Error; err != nil {
			return err
		}

		if err := tx.Save(&neighbor).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update payment status"})
		return
	}

	c.JSON(http.StatusOK, reading)
}

// ============================================================================
// JWT Authentication & Claims
// ============================================================================

type Claims struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	FullName    string   `json:"full_name,omitempty"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
	Exp         int64    `json:"exp"`
	Iat         int64    `json:"iat"`
}

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "kampung-melati-superapp-jwt-secret-key-2026"
	}
	return []byte(secret)
}

func GenerateToken(user *models.User, roles []string, perms []string) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	hJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	hB64 := base64.RawURLEncoding.EncodeToString(hJSON)

	now := time.Now()
	claims := Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Email:       user.Email,
		FullName:    user.FullName,
		Roles:       roles,
		Permissions: perms,
		Exp:         now.Add(72 * time.Hour).Unix(),
		Iat:         now.Unix(),
	}
	cJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	cB64 := base64.RawURLEncoding.EncodeToString(cJSON)

	data := hB64 + "." + cB64
	mac := hmac.New(sha256.New, getJWTSecret())
	mac.Write([]byte(data))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return data + "." + sigB64, nil
}

func decodeB64URL(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func ValidateToken(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if strings.HasPrefix(strings.ToLower(tokenString), "bearer ") {
		tokenString = strings.TrimSpace(tokenString[7:])
	}

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format: must contain 3 segments")
	}

	hJSON, err := decodeB64URL(parts[0])
	if err != nil {
		return nil, errors.New("invalid header encoding")
	}
	var header map[string]interface{}
	if err := json.Unmarshal(hJSON, &header); err != nil {
		return nil, errors.New("invalid header json")
	}
	alg, ok := header["alg"].(string)
	if !ok || alg != "HS256" {
		return nil, errors.New("unsupported signing algorithm")
	}

	expectedMAC := hmac.New(sha256.New, getJWTSecret())
	expectedMAC.Write([]byte(parts[0] + "." + parts[1]))
	expectedSig := expectedMAC.Sum(nil)

	actualSig, err := decodeB64URL(parts[2])
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, errors.New("signature verification failed")
	}

	pJSON, err := decodeB64URL(parts[1])
	if err != nil {
		return nil, errors.New("invalid payload encoding")
	}

	var claims Claims
	if err := json.Unmarshal(pJSON, &claims); err != nil {
		return nil, errors.New("invalid claims json")
	}

	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

// ============================================================================
// Gin RBAC Middlewares
// ============================================================================

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is required"})
			return
		}

		claims, err := ValidateToken(authHeader)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return
		}

		c.Set("claims", claims)
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("roles", claims.Roles)
		c.Set("permissions", claims.Permissions)
		c.Next()
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claimsVal, exists := c.Get("claims")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		claims, ok := claimsVal.(*Claims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		hasRole := false
		for _, userRole := range claims.Roles {
			if userRole == "admin" {
				hasRole = true
				break
			}
			for _, reqRole := range roles {
				if userRole == reqRole {
					hasRole = true
					break
				}
			}
			if hasRole {
				break
			}
		}

		if !hasRole {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: insufficient role"})
			return
		}
		c.Next()
	}
}

func RequirePermission(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claimsVal, exists := c.Get("claims")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		claims, ok := claimsVal.(*Claims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		for _, r := range claims.Roles {
			if r == "admin" {
				c.Next()
				return
			}
		}

		userPerms := make(map[string]bool)
		for _, p := range claims.Permissions {
			userPerms[p] = true
		}

		for _, req := range perms {
			if !userPerms[req] {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: missing permission " + req})
				return
			}
		}

		c.Next()
	}
}

// ============================================================================
// Auth Handlers
// ============================================================================

func Login(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	identifier := input.Email
	if identifier == "" {
		identifier = input.Username
	}
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email or username required"})
		return
	}

	var user models.User
	if err := database.DB.Preload("Roles.Permissions").Where("email = ? OR username = ?", identifier, identifier).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	var roleNames []string
	permSet := make(map[string]bool)
	for _, r := range user.Roles {
		roleNames = append(roleNames, r.Name)
		for _, p := range r.Permissions {
			permSet[p.Code] = true
		}
	}
	var permCodes []string
	for p := range permSet {
		permCodes = append(permCodes, p)
	}

	token, err := GenerateToken(&user, roleNames, permCodes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":        token,
		"access_token": token,
		"user": gin.H{
			"id":          user.ID,
			"username":    user.Username,
			"email":       user.Email,
			"full_name":   user.FullName,
			"neighbor_id": user.NeighborID,
			"roles":       roleNames,
			"permissions": permCodes,
		},
	})
}

func GetMe(c *gin.Context) {
	claimsVal, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	claims := claimsVal.(*Claims)

	var user models.User
	if err := database.DB.Preload("Roles.Permissions").First(&user, "id = ?", claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var roleNames []string
	permSet := make(map[string]bool)
	for _, r := range user.Roles {
		roleNames = append(roleNames, r.Name)
		for _, p := range r.Permissions {
			permSet[p.Code] = true
		}
	}
	var permCodes []string
	for p := range permSet {
		permCodes = append(permCodes, p)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          user.ID,
		"username":    user.Username,
		"email":       user.Email,
		"full_name":   user.FullName,
		"neighbor_id": user.NeighborID,
		"roles":       roleNames,
		"permissions": permCodes,
	})
}

// ============================================================================
// Treasury Handlers
// ============================================================================

func CreateTransaction(c *gin.Context) {
	claimsVal, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	claims := claimsVal.(*Claims)

	var input struct {
		Type            string   `json:"type"`
		Category        string   `json:"category"`
		Amount          *float64 `json:"amount"`
		Direction       string   `json:"direction"`
		Status          string   `json:"status"`
		Period          string   `json:"period"`
		PaymentMethod   string   `json:"payment_method"`
		Description     string   `json:"description"`
		TransactionDate string   `json:"transaction_date"`
		Date            string   `json:"date"`
		UserID          *string  `json:"user_id"`
		NeighborID      *string  `json:"neighbor_id"`
		ReadingID       *string  `json:"reading_id"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	if input.Type == "" || input.Amount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required fields: type and amount"})
		return
	}

	if *input.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be strictly greater than zero"})
		return
	}

	if strings.TrimSpace(input.Description) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Description is required and cannot be empty"})
		return
	}

	direction := input.Direction
	if direction == "" {
		if input.Type == "expenditure" {
			direction = "expense"
		} else {
			direction = "income"
		}
	}

	status := input.Status
	if status == "" {
		status = "completed"
	}

	txDate := time.Now()
	dateStr := input.TransactionDate
	if dateStr == "" {
		dateStr = input.Date
	}
	if dateStr != "" {
		if parsed, err := time.Parse(time.RFC3339, dateStr); err == nil {
			txDate = parsed
		}
	}

	period := input.Period
	if period == "" {
		period = txDate.Format("2006-01")
	}

	txnNumber := fmt.Sprintf("TXN-%d-%s", time.Now().UnixNano(), uuid.New().String()[:8])

	transaction := models.Transaction{
		TransactionNumber: txnNumber,
		Type:              input.Type,
		Direction:         direction,
		Category:          input.Category,
		Amount:            *input.Amount,
		Status:            status,
		Period:            period,
		PaymentMethod:     input.PaymentMethod,
		Description:       input.Description,
		Date:              txDate,
		UserID:            input.UserID,
		NeighborID:        input.NeighborID,
		ReadingID:         input.ReadingID,
		RecordedByID:      claims.UserID,
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if input.ReadingID != nil && *input.ReadingID != "" {
			var reading models.Reading
			if err := tx.First(&reading, "id = ?", *input.ReadingID).Error; err != nil {
				return errors.New("reading_not_found")
			}

			if reading.IsPaid {
				return errors.New("reading_already_paid")
			}

			reading.IsPaid = true
			if err := tx.Save(&reading).Error; err != nil {
				return err
			}

			var neighbor models.Neighbor
			if err := tx.First(&neighbor, "id = ?", reading.NeighborID).Error; err == nil {
				neighbor.TotalUnpaid -= reading.TotalCost
				if neighbor.TotalUnpaid < 0 {
					neighbor.TotalUnpaid = 0
				}
				if err := tx.Save(&neighbor).Error; err != nil {
					return err
				}
				transaction.NeighborID = &neighbor.ID
			}
		}

		if err := tx.Create(&transaction).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if err.Error() == "reading_already_paid" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Reading is already paid"})
			return
		}
		if err.Error() == "reading_not_found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Water reading not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create transaction: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, transaction)
}

func ListTransactions(c *gin.Context) {
	query := database.DB.Model(&models.Transaction{})

	if t := c.Query("type"); t != "" {
		query = query.Where("type = ?", t)
	}
	if cat := c.Query("category"); cat != "" {
		query = query.Where("category = ?", cat)
	}
	if period := c.Query("period"); period != "" {
		query = query.Where("period = ?", period)
	}
	if dir := c.Query("direction"); dir != "" {
		query = query.Where("direction = ?", dir)
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if lim, err := strconv.Atoi(limitStr); err == nil && lim > 0 {
			query = query.Limit(lim)
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if off, err := strconv.Atoi(offsetStr); err == nil && off >= 0 {
			query = query.Offset(off)
		}
	}

	var transactions []models.Transaction
	if err := query.Order("date desc, created_at desc").Find(&transactions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch transactions"})
		return
	}

	c.JSON(http.StatusOK, transactions)
}

func GetTreasurySummary(c *gin.Context) {
	period := c.Query("period")

	query := database.DB.Model(&models.Transaction{}).Where("status = ?", "completed")
	if period != "" {
		query = query.Where("period = ?", period)
	}

	var transactions []models.Transaction
	if err := query.Find(&transactions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate summary"})
		return
	}

	var totalIncome, totalExpense, duesCollected, waterCollected float64
	for _, tx := range transactions {
		if tx.Direction == "income" {
			totalIncome += tx.Amount
			if tx.Type == "dues" {
				duesCollected += tx.Amount
			} else if tx.Type == "water_billing" {
				waterCollected += tx.Amount
			}
		} else if tx.Direction == "expense" {
			totalExpense += tx.Amount
		}
	}
	netBalance := totalIncome - totalExpense

	c.JSON(http.StatusOK, gin.H{
		"period":            period,
		"total_income":      totalIncome,
		"total_expense":     totalExpense,
		"net_balance":       netBalance,
		"balance":           netBalance,
		"dues_collected":    duesCollected,
		"water_collected":   waterCollected,
		"transaction_count": len(transactions),
	})
}

func GetMyDues(c *gin.Context) {
	claimsVal, exists := c.Get("claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	claims := claimsVal.(*Claims)
	currentUserID := claims.UserID

	var user models.User
	if err := database.DB.First(&user, "id = ?", currentUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	var transactions []models.Transaction
	database.DB.Where("user_id = ?", currentUserID).Order("date desc").Find(&transactions)

	var totalDuesPaid float64
	for _, tx := range transactions {
		if tx.Type == "dues" && tx.Status == "completed" {
			totalDuesPaid += tx.Amount
		}
	}

	var neighbor *models.Neighbor
	var unpaidWater float64
	if user.NeighborID != nil && *user.NeighborID != "" {
		var n models.Neighbor
		if err := database.DB.Preload("Readings").First(&n, "id = ?", *user.NeighborID).Error; err == nil {
			neighbor = &n
			unpaidWater = n.TotalUnpaid
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":              currentUserID,
		"username":             user.Username,
		"full_name":            user.FullName,
		"neighbor_id":          user.NeighborID,
		"neighbor":             neighbor,
		"transactions":         transactions,
		"dues":                 transactions,
		"total_dues_paid":      totalDuesPaid,
		"water_balance_unpaid": unpaidWater,
		"status":               "up_to_date",
	})
}

// ============================================================================
// Announcement Handlers
// ============================================================================

func ListAnnouncements(c *gin.Context) {
	var announcements []models.Announcement
	if err := database.DB.Order("created_at desc").Find(&announcements).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch announcements"})
		return
	}
	c.JSON(http.StatusOK, announcements)
}

func CreateAnnouncement(c *gin.Context) {
	var input struct {
		Title    string `json:"title" binding:"required"`
		Content  string `json:"content" binding:"required"`
		Category string `json:"category"`
		Priority string `json:"priority"`
		Author   string `json:"author"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	author := input.Author
	if author == "" {
		if claimsVal, exists := c.Get("claims"); exists {
			if claims, ok := claimsVal.(*Claims); ok {
				author = claims.Username
			}
		}
	}
	if author == "" {
		author = "Admin"
	}

	priority := input.Priority
	if priority == "" {
		priority = "Normal"
	}

	announcement := models.Announcement{
		Title:    input.Title,
		Content:  input.Content,
		Category: input.Category,
		Priority: priority,
		Author:   author,
	}

	if err := database.DB.Create(&announcement).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create announcement"})
		return
	}

	c.JSON(http.StatusCreated, announcement)
}
