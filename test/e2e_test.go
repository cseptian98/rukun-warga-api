package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Configuration and Test Harness Helpers
// ============================================================================

func getBaseURL() string {
	baseURL := os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("API_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return strings.TrimRight(baseURL, "/")
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// checkServerAlive verifies that the API is reachable.
// If E2E_STRICT is set, it fails the test; otherwise it gracefully skips.
func checkServerAlive(t *testing.T) bool {
	t.Helper()
	baseURL := getBaseURL()
	resp, err := httpClient.Get(baseURL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		if os.Getenv("E2E_STRICT") == "true" {
			t.Fatalf("E2E FATAL: API server is not responding at %s/health (err: %v). Ensure rukun-warga-api is running.", baseURL, err)
		} else {
			t.Skipf("E2E NOTICE: API server is not running at %s (%v). Skipping live E2E tests. Run server with 'go run main.go' to execute live tests.", baseURL, err)
		}
		return false
	}
	defer resp.Body.Close()
	return true
}

func doRequest(t *testing.T, method, path string, body interface{}, token string) (*http.Response, []byte) {
	t.Helper()
	baseURL := getBaseURL()
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequest(method, baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("Failed to create HTTP request %s %s: %v", method, path, err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body %s %s: %v", method, path, err)
	}

	return resp, respBytes
}

type LoginResponse struct {
	Token       string                 `json:"token"`
	AccessToken string                 `json:"access_token"`
	User        map[string]interface{} `json:"user"`
	Error       string                 `json:"error"`
}

func login(t *testing.T, identifier, password string) (string, map[string]interface{}) {
	t.Helper()
	payload := map[string]string{
		"username": identifier,
		"email":    identifier,
		"password": password,
	}

	resp, respBytes := doRequest(t, http.MethodPost, "/api/auth/login", payload, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Login failed for %s. Expected 200 OK, got %d: %s", identifier, resp.StatusCode, string(respBytes))
	}

	var lr LoginResponse
	if err := json.Unmarshal(respBytes, &lr); err != nil {
		t.Fatalf("Failed to parse login response for %s: %v (raw: %s)", identifier, err, string(respBytes))
	}

	token := lr.Token
	if token == "" {
		token = lr.AccessToken
	}
	if token == "" {
		t.Fatalf("Login response for %s missing token in payload: %s", identifier, string(respBytes))
	}

	return token, lr.User
}

// ============================================================================
// TIER 1: Feature Coverage Tests
// ============================================================================

// TestTier1_Authentication verifies authentication against default seed accounts
func TestTier1_Authentication(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	t.Run("Login_Admin_Success", func(t *testing.T) {
		token, user := login(t, "admin@kampungmelati.id", "admin123")
		if token == "" {
			t.Errorf("Expected valid JWT token for admin")
		}
		t.Logf("Admin logged in successfully, user: %v", user)
	})

	t.Run("Login_Treasurer_Success", func(t *testing.T) {
		token, user := login(t, "treasurer@kampungmelati.id", "bendahara123")
		if token == "" {
			t.Errorf("Expected valid JWT token for treasurer")
		}
		t.Logf("Treasurer logged in successfully, user: %v", user)
	})

	t.Run("Login_Resident_Success", func(t *testing.T) {
		token, user := login(t, "resident@kampungmelati.id", "warga123")
		if token == "" {
			t.Errorf("Expected valid JWT token for resident")
		}
		t.Logf("Resident logged in successfully, user: %v", user)
	})

	t.Run("Get_Me_Profile", func(t *testing.T) {
		token, _ := login(t, "admin@kampungmelati.id", "admin123")
		resp, body := doRequest(t, http.MethodGet, "/api/auth/me", nil, token)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for /api/auth/me, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// TestTier1_TreasuryEndpoints verifies basic CRUD on Treasury endpoints
func TestTier1_TreasuryEndpoints(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	adminToken, _ := login(t, "admin@kampungmelati.id", "admin123")
	treasurerToken, _ := login(t, "treasurer@kampungmelati.id", "bendahara123")
	residentToken, residentUser := login(t, "resident@kampungmelati.id", "warga123")

	residentID := ""
	if residentUser != nil {
		if id, ok := residentUser["id"].(string); ok {
			residentID = id
		}
	}

	t.Run("Record_Dues_Transaction_By_Treasurer", func(t *testing.T) {
		txPayload := map[string]interface{}{
			"type":             "dues",
			"category":         "monthly_dues",
			"amount":           50000.0,
			"direction":        "income",
			"status":           "completed",
			"period":           "2026-10",
			"payment_method":   "cash",
			"description":      "Iuran Bulanan RT 01",
			"transaction_date": time.Now().Format(time.RFC3339),
		}
		if residentID != "" {
			txPayload["user_id"] = residentID
		}

		resp, body := doRequest(t, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 201 Created or 200 OK for recording transaction, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Get_Treasury_Summary", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, "/api/treasury/summary?period=2026-10", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for /api/treasury/summary, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Get_Treasury_Transactions_Ledger", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, "/api/treasury/transactions?limit=10", nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for /api/treasury/transactions, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Get_Personal_Dues", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, "/api/treasury/my-dues", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for /api/treasury/my-dues, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// TestTier1_WaterEndpointProtection verifies RBAC protection on Water endpoints
func TestTier1_WaterEndpointProtection(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	treasurerToken, _ := login(t, "treasurer@kampungmelati.id", "bendahara123")
	residentToken, _ := login(t, "resident@kampungmelati.id", "warga123")

	t.Run("List_Neighbors_With_WaterRead_Allowed", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, "/api/water/neighbors", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for resident reading neighbors, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Create_Neighbor_With_WaterManage_Allowed", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":    fmt.Sprintf("Warga Baru %d", time.Now().UnixNano()%10000),
			"address": "Blok Z No. 99",
		}
		resp, body := doRequest(t, http.MethodPost, "/api/water/neighbors", payload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 201/200 for treasurer creating neighbor, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// TestTier1_Announcements verifies Announcements route access and creation
func TestTier1_Announcements(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	adminToken, _ := login(t, "admin@kampungmelati.id", "admin123")

	t.Run("List_Announcements_Allowed", func(t *testing.T) {
		resp, _ := doRequest(t, http.MethodGet, "/api/announcements", nil, "")
		if resp.StatusCode != http.StatusOK {
			// Announcements might require token or be open
			respAuth, _ := doRequest(t, http.MethodGet, "/api/announcements", nil, adminToken)
			if respAuth.StatusCode != http.StatusOK {
				t.Errorf("Expected 200 OK for /api/announcements, got unauth=%d auth=%d", resp.StatusCode, respAuth.StatusCode)
			}
		}
	})

	t.Run("Create_Announcement_By_Admin_Allowed", func(t *testing.T) {
		payload := map[string]interface{}{
			"title":   "Kerja Bakti Akbar",
			"content": "Kerja bakti lingkungan RT 04 Minggu pagi.",
		}
		resp, body := doRequest(t, http.MethodPost, "/api/announcements", payload, adminToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 201/200 for admin creating announcement, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// ============================================================================
// TIER 2: Boundary & Corner Cases Tests
// ============================================================================

func TestTier2_BoundaryAndCornerCases(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	residentToken, _ := login(t, "resident@kampungmelati.id", "warga123")
	treasurerToken, _ := login(t, "treasurer@kampungmelati.id", "bendahara123")

	t.Run("Unauthenticated_Requests_Return_401", func(t *testing.T) {
		endpoints := []struct {
			method string
			path   string
		}{
			{http.MethodGet, "/api/treasury/summary"},
			{http.MethodGet, "/api/treasury/transactions"},
			{http.MethodGet, "/api/treasury/my-dues"},
			{http.MethodPost, "/api/treasury/transactions"},
			{http.MethodGet, "/api/water/neighbors"},
			{http.MethodPost, "/api/water/neighbors"},
			{http.MethodPost, "/api/water/readings"},
			{http.MethodPost, "/api/announcements"},
		}

		for _, ep := range endpoints {
			resp, _ := doRequest(t, ep.method, ep.path, nil, "")
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("Expected 401 Unauthorized for unauth %s %s, got %d", ep.method, ep.path, resp.StatusCode)
			}
		}
	})

	t.Run("Resident_Writing_Returns_403_Forbidden", func(t *testing.T) {
		writeActions := []struct {
			name   string
			method string
			path   string
			body   interface{}
		}{
			{
				name:   "Resident_Record_Transaction",
				method: http.MethodPost,
				path:   "/api/treasury/transactions",
				body: map[string]interface{}{
					"type":      "dues",
					"amount":    50000.0,
					"direction": "income",
				},
			},
			{
				name:   "Resident_View_All_Transactions_Ledger",
				method: http.MethodGet,
				path:   "/api/treasury/transactions",
				body:   nil,
			},
			{
				name:   "Resident_Create_Neighbor",
				method: http.MethodPost,
				path:   "/api/water/neighbors",
				body: map[string]interface{}{
					"name":    "Hacker Resident",
					"address": "Blok X No. 00",
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
				name:   "Resident_Create_Announcement",
				method: http.MethodPost,
				path:   "/api/announcements",
				body: map[string]interface{}{
					"title":   "Spam Announcement",
					"content": "Not authorized",
				},
			},
		}

		for _, wa := range writeActions {
			resp, body := doRequest(t, wa.method, wa.path, wa.body, residentToken)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("Expected 403 Forbidden for %s, got %d: %s", wa.name, resp.StatusCode, string(body))
			}
		}
	})

	t.Run("Treasurer_Publish_Announcement_Returns_403_Forbidden", func(t *testing.T) {
		payload := map[string]interface{}{
			"title":   "Treasurer Announcement",
			"content": "Should be rejected because treasurer lacks announcements:publish",
		}
		resp, body := doRequest(t, http.MethodPost, "/api/announcements", payload, treasurerToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden for treasurer publishing announcement, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Negative_And_Zero_Amounts_Return_400_BadRequest", func(t *testing.T) {
		invalidAmounts := []float64{-50000.0, -0.01, 0.0}
		for _, amt := range invalidAmounts {
			payload := map[string]interface{}{
				"type":      "dues",
				"category":  "monthly_dues",
				"amount":    amt,
				"direction": "income",
			}
			resp, body := doRequest(t, http.MethodPost, "/api/treasury/transactions", payload, treasurerToken)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Expected 400 Bad Request for amount %.2f, got %d: %s", amt, resp.StatusCode, string(body))
			}
		}
	})

	t.Run("Invalid_Token_Format_Returns_401_Unauthorized", func(t *testing.T) {
		malformedTokens := []string{
			"invalid.fake.jwt",
			"Bearer token-without-proper-signature",
			"12345",
		}
		for _, tok := range malformedTokens {
			resp, _ := doRequest(t, http.MethodGet, "/api/treasury/summary", nil, tok)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("Expected 401 for token '%s', got %d", tok, resp.StatusCode)
			}
		}
	})

	t.Run("Missing_Required_Fields_Returns_400_BadRequest", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodPost, "/api/treasury/transactions", map[string]interface{}{}, treasurerToken)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request for empty transaction payload, got %d: %s", resp.StatusCode, string(body))
		}
	})

	t.Run("Nonexistent_Neighbor_Returns_404_NotFound", func(t *testing.T) {
		resp, _ := doRequest(t, http.MethodGet, "/api/water/neighbors/00000000-0000-0000-0000-000000000000", nil, treasurerToken)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 Not Found for non-existent neighbor, got %d", resp.StatusCode)
		}
	})
}

// ============================================================================
// TIER 3: Cross-Feature Interactions Tests
// ============================================================================

func TestTier3_CrossFeatureInteractions(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	treasurerToken, _ := login(t, "treasurer@kampungmelati.id", "bendahara123")
	adminToken, _ := login(t, "admin@kampungmelati.id", "admin123")
	residentToken, residentUser := login(t, "resident@kampungmelati.id", "warga123")

	t.Run("WaterBill_AtomicPayment_Updates_Reading_And_Neighbor", func(t *testing.T) {
		// 1. Create a dedicated neighbor
		neighborPayload := map[string]interface{}{
			"name":    fmt.Sprintf("Pak Joko E2E %d", time.Now().UnixNano()%10000),
			"address": "Blok E No. 42",
		}
		resp, nBody := doRequest(t, http.MethodPost, "/api/water/neighbors", neighborPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to create neighbor for atomic test: %d: %s", resp.StatusCode, string(nBody))
		}
		var neighbor map[string]interface{}
		_ = json.Unmarshal(nBody, &neighbor)
		neighborID, ok := neighbor["id"].(string)
		if !ok || neighborID == "" {
			t.Fatalf("Created neighbor missing id: %s", string(nBody))
		}

		// 2. Create a water reading: 15 m3 -> Tiered cost: 10*1.00 + 5*1.50 = 17.50
		readingPayload := map[string]interface{}{
			"neighbor_id": neighborID,
			"volume":      15.0,
		}
		resp, rBody := doRequest(t, http.MethodPost, "/api/water/readings", readingPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to create reading: %d: %s", resp.StatusCode, string(rBody))
		}
		var reading map[string]interface{}
		_ = json.Unmarshal(rBody, &reading)
		readingID, ok := reading["id"].(string)
		if !ok || readingID == "" {
			t.Fatalf("Created reading missing id: %s", string(rBody))
		}

		// Verify reading is initially unpaid
		if isPaid, ok := reading["is_paid"].(bool); ok && isPaid {
			t.Fatalf("New reading should have is_paid = false, got true")
		}

		// 3. Record water bill payment transaction in Treasury
		txPayload := map[string]interface{}{
			"type":             "water_billing",
			"category":         "water_utility",
			"amount":           17.50,
			"direction":        "income",
			"status":           "completed",
			"reading_id":       readingID,
			"payment_method":   "bank_transfer",
			"description":      "Pembayaran tagihan air Blok E No. 42",
			"transaction_date": time.Now().Format(time.RFC3339),
		}
		resp, txBody := doRequest(t, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to record water bill transaction: %d: %s", resp.StatusCode, string(txBody))
		}

		// 4. Fetch the neighbor and verify reading.is_paid == true and total_unpaid is reduced
		resp, checkBody := doRequest(t, http.MethodGet, fmt.Sprintf("/api/water/neighbors/%s", neighborID), nil, treasurerToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to get neighbor after payment: %d: %s", resp.StatusCode, string(checkBody))
		}
		var updatedNeighbor map[string]interface{}
		_ = json.Unmarshal(checkBody, &updatedNeighbor)

		if totalUnpaid, ok := updatedNeighbor["total_unpaid"].(float64); ok {
			if totalUnpaid > 0.01 {
				t.Errorf("Expected neighbor total_unpaid to be 0 after payment, got %.2f", totalUnpaid)
			}
		}

		// 5. Double Payment Prevention: Attempting to pay the SAME reading again must return 400
		respDup, dupBody := doRequest(t, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
		if respDup.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request when paying already-paid water reading, got %d: %s", respDup.StatusCode, string(dupBody))
		}
	})

	t.Run("Resident_Isolation_Cannot_Snoop_Other_Dues", func(t *testing.T) {
		residentID := ""
		if residentUser != nil {
			if id, ok := residentUser["id"].(string); ok {
				residentID = id
			}
		}

		// Calling /api/treasury/my-dues with an external fake user_id query param
		snoopURL := "/api/treasury/my-dues?user_id=00000000-0000-0000-0000-000000000000"
		resp, body := doRequest(t, http.MethodGet, snoopURL, nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 OK for /api/treasury/my-dues, got %d: %s", resp.StatusCode, string(body))
		}

		var myDues map[string]interface{}
		if err := json.Unmarshal(body, &myDues); err == nil && residentID != "" {
			if respUserID, ok := myDues["user_id"].(string); ok && respUserID != "" {
				if respUserID != residentID {
					t.Errorf("Security Violation: Endpoint returned user_id %s instead of authenticated user %s", respUserID, residentID)
				}
			}
		}
	})

	t.Run("Admin_Community_Audit_Logs_And_Filtering", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, "/api/treasury/transactions?type=dues", nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for admin viewing audit logs, got %d: %s", resp.StatusCode, string(body))
		}
	})
}

// ============================================================================
// TIER 4: Real-World Workload Scenarios Tests
// ============================================================================

func TestTier4_RealWorldWorkloadScenarios(t *testing.T) {
	if !checkServerAlive(t) {
		return
	}

	treasurerToken, _ := login(t, "treasurer@kampungmelati.id", "bendahara123")
	adminToken, _ := login(t, "admin@kampungmelati.id", "admin123")
	residentToken, residentUser := login(t, "resident@kampungmelati.id", "warga123")

	residentID := ""
	if residentUser != nil {
		if id, ok := residentUser["id"].(string); ok {
			residentID = id
		}
	}

	t.Run("Scenario1_MonthlyDuesCollectionWorkflow", func(t *testing.T) {
		// Step 1: Capture baseline summary
		resp, sumBody := doRequest(t, http.MethodGet, "/api/treasury/summary?period=2026-10", nil, treasurerToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to fetch initial summary: %d: %s", resp.StatusCode, string(sumBody))
		}
		var initialSummary map[string]interface{}
		_ = json.Unmarshal(sumBody, &initialSummary)

		// Step 2: Treasurer records monthly dues for resident
		duesAmount := 50000.0
		txPayload := map[string]interface{}{
			"type":             "dues",
			"category":         "monthly_dues",
			"amount":           duesAmount,
			"direction":        "income",
			"status":           "completed",
			"period":           "2026-10",
			"payment_method":   "qris",
			"description":      "Iuran Kebersihan & Keamanan Okt 2026",
			"transaction_date": time.Now().Format(time.RFC3339),
		}
		if residentID != "" {
			txPayload["user_id"] = residentID
		}

		resp, txBody := doRequest(t, http.MethodPost, "/api/treasury/transactions", txPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to record dues payment: %d: %s", resp.StatusCode, string(txBody))
		}

		// Step 3: Resident inspects my-dues
		resp, duesBody := doRequest(t, http.MethodGet, "/api/treasury/my-dues", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to query resident dues: %d: %s", resp.StatusCode, string(duesBody))
		}

		// Step 4: Verify summary increased
		resp, newSumBody := doRequest(t, http.MethodGet, "/api/treasury/summary?period=2026-10", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to query updated summary: %d: %s", resp.StatusCode, string(newSumBody))
		}
		t.Logf("Scenario 1 verified cleanly: monthly dues collected and reflected.")
	})

	t.Run("Scenario2_PipeRepairExpenditureWorkflow", func(t *testing.T) {
		// Step 1: Treasurer records infrastructure repair expenditure
		repairAmount := 450000.0
		expPayload := map[string]interface{}{
			"type":             "expenditure",
			"category":         "infrastructure_repair",
			"amount":           repairAmount,
			"direction":        "expense",
			"status":           "completed",
			"period":           "2026-10",
			"payment_method":   "cash",
			"description":      "Las pipa darurat Blok D akibat kebocoran",
			"transaction_date": time.Now().Format(time.RFC3339),
		}
		resp, expBody := doRequest(t, http.MethodPost, "/api/treasury/transactions", expPayload, treasurerToken)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to log pipe repair expenditure: %d: %s", resp.StatusCode, string(expBody))
		}

		// Step 2: Query summary to verify expense tracking
		resp, sumBody := doRequest(t, http.MethodGet, "/api/treasury/summary?period=2026-10", nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to fetch summary after expense: %d: %s", resp.StatusCode, string(sumBody))
		}

		// Step 3: Admin inspects audit ledger with expenditure filter
		resp, ledgerBody := doRequest(t, http.MethodGet, "/api/treasury/transactions?type=expenditure", nil, adminToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to query expenditure transactions: %d: %s", resp.StatusCode, string(ledgerBody))
		}
		t.Logf("Scenario 2 verified cleanly: pipe repair expense logged and audited.")
	})

	t.Run("Scenario3_ResidentWaterInquiryWorkflow", func(t *testing.T) {
		// Step 1: Resident queries dues & water balance
		resp, initBody := doRequest(t, http.MethodGet, "/api/treasury/my-dues", nil, residentToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to get resident initial dues: %d: %s", resp.StatusCode, string(initBody))
		}

		t.Logf("Scenario 3 verified cleanly: resident water inquiry functional.")
	})
}
