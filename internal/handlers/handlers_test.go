package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cseptian/rukun-warga-api/internal/database"
	"github.com/cseptian/rukun-warga-api/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test_rw.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open sqlite test db: %v", err)
	}

	err = db.AutoMigrate(
		&models.Neighbor{},
		&models.Reading{},
		&models.Permission{},
		&models.Role{},
		&models.User{},
		&models.Transaction{},
		&models.Announcement{},
	)
	if err != nil {
		t.Fatalf("Failed to auto-migrate sqlite test db: %v", err)
	}

	if err := database.AutoMigrateAndSeed(db); err != nil {
		t.Fatalf("Failed to run AutoMigrateAndSeed: %v", err)
	}

	if err := seedPersonaAccounts(db); err != nil {
		t.Fatalf("Failed to seed persona accounts: %v", err)
	}

	database.DB = db
	return db
}

func seedPersonaAccounts(db *gorm.DB) error {
	var budiNeighbor models.Neighbor
	var budiNeighborID *string
	if err := db.Where("name = ?", "Budi Santoso").First(&budiNeighbor).Error; err != nil {
		budiNeighbor = models.Neighbor{
			Name:        "Budi Santoso",
			Address:     "RT 02 / RW 05 No. 10",
			JoinDate:    time.Now(),
			TotalUnpaid: 0,
		}
		if err := db.Create(&budiNeighbor).Error; err == nil {
			budiNeighborID = &budiNeighbor.ID
		}
	} else {
		budiNeighborID = &budiNeighbor.ID
	}

	var roles []models.Role
	db.Preload("Permissions").Find(&roles)
	roleMap := make(map[string]models.Role)
	for _, r := range roles {
		roleMap[r.Name] = r
	}

	personaDefs := []struct {
		Username   string
		Email      string
		Password   string
		FullName   string
		RoleName   string
		NeighborID *string
	}{
		{
			Username:   "hartono",
			Email:      "rw05.hartono@kampungmelati.id",
			Password:   "admin123",
			FullName:   "Bapak Hartono (Ketua RW 05)",
			RoleName:   "admin",
			NeighborID: nil,
		},
		{
			Username:   "ratna",
			Email:      "bendahara.ratna@kampungmelati.id",
			Password:   "bendahara123",
			FullName:   "Ibu Ratna (Bendahara RW)",
			RoleName:   "treasurer",
			NeighborID: nil,
		},
		{
			Username:   "budi",
			Email:      "budi.santoso@gmail.com",
			Password:   "warga123",
			FullName:   "Mas Budi Santoso (Warga RT 02)",
			RoleName:   "resident",
			NeighborID: budiNeighborID,
		},
	}

	for _, pd := range personaDefs {
		hash, err := bcrypt.GenerateFromPassword([]byte(pd.Password), 10)
		if err != nil {
			return err
		}
		var user models.User
		if err := db.Where("email = ? OR username = ?", pd.Email, pd.Username).First(&user).Error; err != nil {
			user = models.User{
				Username:     pd.Username,
				Email:        pd.Email,
				PasswordHash: string(hash),
				FullName:     pd.FullName,
				NeighborID:   pd.NeighborID,
			}
			if err := db.Create(&user).Error; err != nil {
				return err
			}
		} else {
			user.PasswordHash = string(hash)
			user.FullName = pd.FullName
			if pd.NeighborID != nil {
				user.NeighborID = pd.NeighborID
			}
			db.Save(&user)
		}
		if role, ok := roleMap[pd.RoleName]; ok {
			_ = db.Model(&user).Association("Roles").Replace([]models.Role{role})
		}
	}
	return nil
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	api := r.Group("/api")
	{
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", Login)
			authGroup.GET("/me", AuthMiddleware(), GetMe)
		}

		water := api.Group("/water")
		water.Use(AuthMiddleware())
		{
			water.GET("/neighbors", RequirePermission("water:read"), ListNeighbors)
			water.GET("/neighbors/:id", RequirePermission("water:read"), GetNeighbor)
			water.POST("/neighbors", RequirePermission("water:manage"), CreateNeighbor)
			water.POST("/readings", RequirePermission("water:manage"), CreateReading)
			water.PATCH("/readings/:id/toggle-payment", RequirePermission("water:manage"), TogglePaymentStatus)
		}

		treasury := api.Group("/treasury")
		treasury.Use(AuthMiddleware())
		{
			treasury.GET("/summary", RequirePermission("treasury:read"), GetTreasurySummary)
			treasury.GET("/my-dues", RequirePermission("treasury:read"), GetMyDues)
			treasury.GET("/transactions", RequirePermission("treasury:write"), ListTransactions)
			treasury.POST("/transactions", RequirePermission("treasury:write"), CreateTransaction)
		}

		api.GET("/announcements", ListAnnouncements)
		api.POST("/announcements", AuthMiddleware(), RequirePermission("announcements:publish"), CreateAnnouncement)
	}

	return r
}

func executeRequest(r *gin.Engine, method, path string, body interface{}, token string) (*httptest.ResponseRecorder, map[string]interface{}) {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var respJSON map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &respJSON)
	return w, respJSON
}

func loginHelper(t *testing.T, r *gin.Engine, identifier, password string) (string, map[string]interface{}) {
	payload := map[string]string{
		"username": identifier,
		"email":    identifier,
		"password": password,
	}
	w, respJSON := executeRequest(r, http.MethodPost, "/api/auth/login", payload, "")
	if w.Code != http.StatusOK {
		t.Fatalf("Login failed for %s with status %d: %s", identifier, w.Code, w.Body.String())
	}
	token, ok := respJSON["token"].(string)
	if !ok || token == "" {
		t.Fatalf("Login succeeded but token is missing for %s", identifier)
	}
	userObj, _ := respJSON["user"].(map[string]interface{})
	return token, userObj
}

// ============================================================================
// 1. Authentication Endpoint Tests
// ============================================================================

func TestAuthEndpoints(t *testing.T) {
	setupTestDB(t)
	r := setupTestRouter()

	t.Run("Login_Hartono_ByEmail_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "rw05.hartono@kampungmelati.id", "admin123")
		if token == "" {
			t.Errorf("Expected non-empty token")
		}
		if userObj["username"] != "hartono" {
			t.Errorf("Expected username 'hartono', got '%v'", userObj["username"])
		}

		claims, err := ValidateToken(token)
		if err != nil {
			t.Fatalf("Failed to validate issued token: %v", err)
		}
		hasAdmin := false
		for _, role := range claims.Roles {
			if role == "admin" {
				hasAdmin = true
			}
		}
		if !hasAdmin {
			t.Errorf("Expected claims to include 'admin' role, got %v", claims.Roles)
		}
		expectedPerms := []string{"treasury:read", "treasury:write", "water:manage", "announcements:publish", "water:read"}
		for _, exp := range expectedPerms {
			found := false
			for _, p := range claims.Permissions {
				if p == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected admin to have permission %s, missing from %v", exp, claims.Permissions)
			}
		}
	})

	t.Run("Login_Hartono_ByUsername_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "hartono", "admin123")
		if token == "" || userObj["email"] != "rw05.hartono@kampungmelati.id" {
			t.Errorf("Failed to log in Hartono by username: %v", userObj)
		}
	})

	t.Run("Login_Ratna_ByEmail_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "bendahara.ratna@kampungmelati.id", "bendahara123")
		if token == "" {
			t.Errorf("Expected non-empty token")
		}
		if userObj["username"] != "ratna" {
			t.Errorf("Expected username 'ratna', got '%v'", userObj["username"])
		}

		claims, err := ValidateToken(token)
		if err != nil {
			t.Fatalf("Failed to validate issued token: %v", err)
		}
		hasTreasurer := false
		for _, role := range claims.Roles {
			if role == "treasurer" {
				hasTreasurer = true
			}
		}
		if !hasTreasurer {
			t.Errorf("Expected claims to include 'treasurer' role, got %v", claims.Roles)
		}
		// Treasurer must NOT have announcements:publish
		for _, p := range claims.Permissions {
			if p == "announcements:publish" {
				t.Errorf("Treasurer should not have announcements:publish permission")
			}
		}
	})

	t.Run("Login_Ratna_ByUsername_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "ratna", "bendahara123")
		if token == "" || userObj["email"] != "bendahara.ratna@kampungmelati.id" {
			t.Errorf("Failed to log in Ratna by username: %v", userObj)
		}
	})

	t.Run("Login_Budi_ByEmail_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "budi.santoso@gmail.com", "warga123")
		if token == "" {
			t.Errorf("Expected non-empty token")
		}
		if userObj["username"] != "budi" {
			t.Errorf("Expected username 'budi', got '%v'", userObj["username"])
		}
		if userObj["neighbor_id"] == nil || userObj["neighbor_id"] == "" {
			t.Errorf("Expected resident Budi to be linked to neighbor_id, got nil")
		}

		claims, err := ValidateToken(token)
		if err != nil {
			t.Fatalf("Failed to validate issued token: %v", err)
		}
		hasResident := false
		for _, role := range claims.Roles {
			if role == "resident" {
				hasResident = true
			}
		}
		if !hasResident {
			t.Errorf("Expected claims to include 'resident' role, got %v", claims.Roles)
		}
		// Resident should not have write permissions
		for _, p := range claims.Permissions {
			if p == "treasury:write" || p == "water:manage" || p == "announcements:publish" {
				t.Errorf("Resident should not have write permission %s", p)
			}
		}
	})

	t.Run("Login_Budi_ByUsername_Success", func(t *testing.T) {
		token, userObj := loginHelper(t, r, "budi", "warga123")
		if token == "" || userObj["email"] != "budi.santoso@gmail.com" {
			t.Errorf("Failed to log in Budi by username: %v", userObj)
		}
	})

	t.Run("Login_Existing_Default_Accounts_Success", func(t *testing.T) {
		adminTok, _ := loginHelper(t, r, "admin@kampungmelati.id", "admin123")
		if adminTok == "" {
			t.Errorf("Expected valid token for existing admin")
		}
		treasurerTok, _ := loginHelper(t, r, "treasurer@kampungmelati.id", "bendahara123")
		if treasurerTok == "" {
			t.Errorf("Expected valid token for existing treasurer")
		}
		residentTok, _ := loginHelper(t, r, "resident@kampungmelati.id", "warga123")
		if residentTok == "" {
			t.Errorf("Expected valid token for existing resident")
		}
	})

	t.Run("Login_InvalidCredentials_WrongPassword_Returns_401", func(t *testing.T) {
		payload := map[string]string{
			"email":    "rw05.hartono@kampungmelati.id",
			"password": "wrongpassword999",
		}
		w, resp := executeRequest(r, http.MethodPost, "/api/auth/login", payload, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized for wrong password, got %d", w.Code)
		}
		if resp["error"] != "Invalid credentials" {
			t.Errorf("Expected 'Invalid credentials' error message, got '%v'", resp["error"])
		}
	})

	t.Run("Login_InvalidCredentials_UnknownUser_Returns_401", func(t *testing.T) {
		payload := map[string]string{
			"username": "unknown_user_xyz",
			"password": "somepassword",
		}
		w, resp := executeRequest(r, http.MethodPost, "/api/auth/login", payload, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected 401 Unauthorized for non-existent user, got %d", w.Code)
		}
		if resp["error"] != "Invalid credentials" {
			t.Errorf("Expected 'Invalid credentials' error message, got '%v'", resp["error"])
		}
	})

	t.Run("Login_BadRequest_MissingPassword_Returns_400", func(t *testing.T) {
		payload := map[string]string{
			"username": "hartono",
		}
		w, _ := executeRequest(r, http.MethodPost, "/api/auth/login", payload, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request when password is missing, got %d", w.Code)
		}
	})

	t.Run("Login_BadRequest_MissingIdentifier_Returns_400", func(t *testing.T) {
		payload := map[string]string{
			"password": "admin123",
		}
		w, resp := executeRequest(r, http.MethodPost, "/api/auth/login", payload, "")
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request when username and email are empty, got %d", w.Code)
		}
		if resp["error"] != "Email or username required" {
			t.Errorf("Expected 'Email or username required' error, got '%v'", resp["error"])
		}
	})

	t.Run("GetMe_Profile_Authenticated", func(t *testing.T) {
		token, _ := loginHelper(t, r, "hartono", "admin123")
		w, resp := executeRequest(r, http.MethodGet, "/api/auth/me", nil, token)
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for /api/auth/me, got %d", w.Code)
		}
		if resp["username"] != "hartono" {
			t.Errorf("Expected profile username 'hartono', got '%v'", resp["username"])
		}
	})
}

// ============================================================================
// 2. RBAC Middleware Tests
// ============================================================================

func TestRBACMiddleware(t *testing.T) {
	setupTestDB(t)
	r := setupTestRouter()

	adminToken, _ := loginHelper(t, r, "hartono", "admin123")
	treasurerToken, _ := loginHelper(t, r, "ratna", "bendahara123")
	residentToken, _ := loginHelper(t, r, "budi", "warga123")

	t.Run("Unauthenticated_Requests_Return_401", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/auth/me"},
			{http.MethodGet, "/api/water/neighbors"},
			{http.MethodGet, "/api/treasury/summary"},
			{http.MethodGet, "/api/treasury/my-dues"},
			{http.MethodGet, "/api/treasury/transactions"},
			{http.MethodPost, "/api/treasury/transactions"},
			{http.MethodPost, "/api/water/neighbors"},
			{http.MethodPost, "/api/water/readings"},
			{http.MethodPatch, "/api/water/readings/123/toggle-payment"},
			{http.MethodPost, "/api/announcements"},
		}

		for _, ep := range endpoints {
			w, _ := executeRequest(r, ep.method, ep.path, nil, "")
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized for unauthenticated %s %s, got %d", ep.method, ep.path, w.Code)
			}
		}
	})

	t.Run("Invalid_Or_Malformed_Tokens_Return_401", func(t *testing.T) {
		invalidTokens := []string{
			"malformed-token-string",
			"header.payload.invalidsig",
			"Bearer not-a-jwt",
		}
		for _, badTok := range invalidTokens {
			w, _ := executeRequest(r, http.MethodGet, "/api/treasury/summary", nil, badTok)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected 401 for token '%s', got %d", badTok, w.Code)
			}
		}
	})

	t.Run("Resident_Forbidden_Endpoints_Return_403", func(t *testing.T) {
		forbiddenCalls := []struct {
			name   string
			method string
			path   string
			body   interface{}
		}{
			{
				name:   "Resident_Record_Treasury_Transaction",
				method: http.MethodPost,
				path:   "/api/treasury/transactions",
				body: map[string]interface{}{
					"type":        "dues",
					"amount":      50000.0,
					"description": "Unauthorized attempt",
				},
			},
			{
				name:   "Resident_List_All_Transactions",
				method: http.MethodGet,
				path:   "/api/treasury/transactions",
				body:   nil,
			},
			{
				name:   "Resident_Create_Neighbor",
				method: http.MethodPost,
				path:   "/api/water/neighbors",
				body: map[string]interface{}{
					"name":    "Unauthorized Neighbor",
					"address": "Blok Z No. 99",
				},
			},
			{
				name:   "Resident_Create_Reading",
				method: http.MethodPost,
				path:   "/api/water/readings",
				body: map[string]interface{}{
					"neighbor_id": "00000000-0000-0000-0000-000000000000",
					"volume":      10.0,
				},
			},
			{
				name:   "Resident_Toggle_Payment",
				method: http.MethodPatch,
				path:   "/api/water/readings/00000000-0000-0000-0000-000000000000/toggle-payment",
				body:   nil,
			},
			{
				name:   "Resident_Create_Announcement",
				method: http.MethodPost,
				path:   "/api/announcements",
				body: map[string]interface{}{
					"title":   "Spam Announcement",
					"content": "Resident cannot publish",
				},
			},
		}

		for _, tc := range forbiddenCalls {
			w, _ := executeRequest(r, tc.method, tc.path, tc.body, residentToken)
			if w.Code != http.StatusForbidden {
				t.Errorf("[%s] Expected 403 Forbidden for resident, got %d: %s", tc.name, w.Code, w.Body.String())
			}
		}
	})

	t.Run("Resident_Authorized_Endpoints_Return_200", func(t *testing.T) {
		wSum, _ := executeRequest(r, http.MethodGet, "/api/treasury/summary", nil, residentToken)
		if wSum.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for resident on /api/treasury/summary, got %d", wSum.Code)
		}

		wDues, _ := executeRequest(r, http.MethodGet, "/api/treasury/my-dues", nil, residentToken)
		if wDues.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for resident on /api/treasury/my-dues, got %d", wDues.Code)
		}

		wNeighbors, _ := executeRequest(r, http.MethodGet, "/api/water/neighbors", nil, residentToken)
		if wNeighbors.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for resident on /api/water/neighbors, got %d", wNeighbors.Code)
		}
	})

	t.Run("Admin_And_Treasurer_Access_Treasury_Transactions", func(t *testing.T) {
		wAdmin, _ := executeRequest(r, http.MethodGet, "/api/treasury/transactions", nil, adminToken)
		if wAdmin.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for admin on /api/treasury/transactions, got %d", wAdmin.Code)
		}

		wTreas, _ := executeRequest(r, http.MethodGet, "/api/treasury/transactions", nil, treasurerToken)
		if wTreas.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for treasurer on /api/treasury/transactions, got %d", wTreas.Code)
		}
	})
}

// ============================================================================
// 3. Business Logic Validation Tests
// ============================================================================

func TestBusinessLogicValidation(t *testing.T) {
	setupTestDB(t)
	r := setupTestRouter()

	adminToken, _ := loginHelper(t, r, "hartono", "admin123")
	treasurerToken, _ := loginHelper(t, r, "ratna", "bendahara123")

	t.Run("Treasury_Negative_And_Zero_Amounts_Return_400", func(t *testing.T) {
		invalidAmounts := []float64{-50000.0, -1.0, -0.01, 0.0}
		for _, amt := range invalidAmounts {
			payload := map[string]interface{}{
				"type":        "dues",
				"amount":      amt,
				"description": "Test invalid amount",
			}
			w, resp := executeRequest(r, http.MethodPost, "/api/treasury/transactions", payload, treasurerToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for amount %.2f, got %d: %s", amt, w.Code, w.Body.String())
			}
			if resp["error"] != "Amount must be strictly greater than zero" {
				t.Errorf("Expected error message 'Amount must be strictly greater than zero', got '%v'", resp["error"])
			}
		}
	})

	t.Run("Treasury_Empty_Or_Whitespace_Description_Returns_400", func(t *testing.T) {
		invalidDescriptions := []string{"", "   ", "\t\n"}
		for _, desc := range invalidDescriptions {
			payload := map[string]interface{}{
				"type":        "dues",
				"amount":      100000.0,
				"description": desc,
			}
			w, _ := executeRequest(r, http.MethodPost, "/api/treasury/transactions", payload, treasurerToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for empty/whitespace description '%s', got %d: %s", desc, w.Code, w.Body.String())
			}
		}

		// Missing description field entirely
		payloadNoDesc := map[string]interface{}{
			"type":   "dues",
			"amount": 100000.0,
		}
		w, _ := executeRequest(r, http.MethodPost, "/api/treasury/transactions", payloadNoDesc, treasurerToken)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request when description field is missing, got %d", w.Code)
		}
	})

	t.Run("Water_Reading_Negative_And_Zero_Volume_Returns_400", func(t *testing.T) {
		invalidVolumes := []float64{-25.0, -0.5, 0.0}
		for _, vol := range invalidVolumes {
			payload := map[string]interface{}{
				"neighbor_id": "00000000-0000-0000-0000-000000000000",
				"volume":      vol,
			}
			w, resp := executeRequest(r, http.MethodPost, "/api/water/readings", payload, adminToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for volume %.2f, got %d: %s", vol, w.Code, w.Body.String())
			}
			if resp["error"] != "Volume must be strictly greater than zero" {
				t.Errorf("Expected error 'Volume must be strictly greater than zero', got '%v'", resp["error"])
			}
		}
	})

	t.Run("Water_Neighbor_Empty_And_Whitespace_Inputs_Return_400", func(t *testing.T) {
		cases := []struct {
			name    string
			address string
		}{
			{"", "Blok A No. 1"},
			{"   ", "Blok A No. 1"},
			{"Pak RT", ""},
			{"Pak RT", "   "},
			{"", ""},
		}

		for _, tc := range cases {
			payload := map[string]interface{}{
				"name":    tc.name,
				"address": tc.address,
			}
			w, _ := executeRequest(r, http.MethodPost, "/api/water/neighbors", payload, adminToken)
			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for neighbor name='%s', address='%s', got %d: %s", tc.name, tc.address, w.Code, w.Body.String())
			}
		}
	})
}

// ============================================================================
// 4. Integration Workflow Tests
// ============================================================================

func TestIntegrationWorkflow(t *testing.T) {
	setupTestDB(t)
	r := setupTestRouter()

	adminToken, _ := loginHelper(t, r, "hartono", "admin123")
	treasurerToken, _ := loginHelper(t, r, "ratna", "bendahara123")

	// 1. Create a valid neighbor
	nPayload := map[string]interface{}{
		"name":    "Pak Bambang",
		"address": "RT 03 / RW 05 Blok C No. 7",
	}
	wN, nResp := executeRequest(r, http.MethodPost, "/api/water/neighbors", nPayload, adminToken)
	if wN.Code != http.StatusCreated {
		t.Fatalf("Failed to create neighbor: %d: %s", wN.Code, wN.Body.String())
	}
	neighborID := nResp["id"].(string)

	// 2. Record water reading with volume 15 m3
	// Tiered calculation: 10*1.00 + 5*1.50 = 17.50
	rPayload := map[string]interface{}{
		"neighbor_id": neighborID,
		"volume":      15.0,
	}
	wR, rResp := executeRequest(r, http.MethodPost, "/api/water/readings", rPayload, adminToken)
	if wR.Code != http.StatusCreated {
		t.Fatalf("Failed to create reading: %d: %s", wR.Code, wR.Body.String())
	}
	readingID := rResp["id"].(string)
	cost := rResp["total_cost"].(float64)
	if cost != 17.50 {
		t.Errorf("Expected tiered cost 17.50 for 15 m3, got %.2f", cost)
	}

	// 3. Verify neighbor total_unpaid increased
	wCheck, checkResp := executeRequest(r, http.MethodGet, fmt.Sprintf("/api/water/neighbors/%s", neighborID), nil, treasurerToken)
	if wCheck.Code != http.StatusOK {
		t.Fatalf("Failed to get neighbor: %d", wCheck.Code)
	}
	if unpaid := checkResp["total_unpaid"].(float64); unpaid != 17.50 {
		t.Errorf("Expected total_unpaid 17.50, got %.2f", unpaid)
	}

	// 4. Treasurer records payment transaction linked to reading
	txPayload := map[string]interface{}{
		"type":        "water_billing",
		"amount":      17.50,
		"reading_id":  readingID,
		"description": "Pembayaran air Pak Bambang",
	}
	wTx, _ := executeRequest(r, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
	if wTx.Code != http.StatusCreated && wTx.Code != http.StatusOK {
		t.Fatalf("Failed to record payment transaction: %d: %s", wTx.Code, wTx.Body.String())
	}

	// 5. Verify reading is now paid and neighbor total_unpaid reduced
	wCheck2, checkResp2 := executeRequest(r, http.MethodGet, fmt.Sprintf("/api/water/neighbors/%s", neighborID), nil, treasurerToken)
	if wCheck2.Code != http.StatusOK {
		t.Fatalf("Failed to get neighbor after payment: %d", wCheck2.Code)
	}
	if unpaid := checkResp2["total_unpaid"].(float64); unpaid > 0.01 {
		t.Errorf("Expected total_unpaid ~0 after payment, got %.2f", unpaid)
	}

	// 6. Double payment prevention: attempting to pay the same reading again returns 400
	wDup, _ := executeRequest(r, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
	if wDup.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request when paying already paid reading, got %d", wDup.Code)
	}
}
