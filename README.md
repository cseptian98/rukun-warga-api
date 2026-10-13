# Rukun Warga API (`rukun-warga-api`)

A resilient, scalable Go backend service powering the **Kampung Melati SuperApp** ecosystem. Built with [Gin](https://gin-gonic.com/), [GORM](https://gorm.io/), and [PostgreSQL](https://www.postgresql.org/), it implements centralized Role-Based Access Control (RBAC), utility billing, citizen dues/treasury management, and community announcements.

---

## 🚀 Key Modules & Endpoints

### 1. Authentication & RBAC (`/api/auth`)
- `POST /api/auth/login`: Authenticates users and returns signed HMAC-SHA256 JWT tokens containing role and permission claims.
- `GET /api/auth/me`: Retrieves current session profile and permissions (requires valid JWT).

### 2. Water System Management (`/api/water`)
- `GET /api/water/neighbors`: List all registered households and meter identifiers (`water:read`).
- `GET /api/water/neighbors/:id`: Get detailed meter reading history for a specific neighbor (`water:read`).
- `POST /api/water/neighbors`: Register a new resident household (`water:manage`).
- `POST /api/water/readings`: Record monthly meter readings with tiered consumption calculations (`water:manage`).
- `PATCH /api/water/readings/:id/toggle-payment`: Toggle water bill payment status (`water:manage`).

### 3. Treasury & Kas RW (`/api/treasury`)
- `GET /api/treasury/summary`: Community-wide financial overview (balance, inflow, outflow, dues compliance) (`treasury:read`).
- `GET /api/treasury/my-dues`: Resident-scoped dues compliance history (`treasury:read`).
- `GET /api/treasury/transactions`: Audit ledger of all citizen dues and operational expenditures (`treasury:write`).
- `POST /api/treasury/transactions`: Record income or expenditure transactions (`treasury:write`).

### 4. Announcements & Community Bulletins (`/api/announcements`)
- `GET /api/announcements`: Public community feed (unrestricted).
- `POST /api/announcements`: Publish news and critical broadcast alerts (`announcements:publish`).

---

## 🔐 Permissions & Default Roles

Default seeded roles and permission sets:

| Role | Description | Assigned Permissions |
| :--- | :--- | :--- |
| `admin` | Ketua RW / System Administrator | Bypass all guards / all permissions |
| `treasurer` | Bendahara RW | `treasury:read`, `treasury:write`, `water:read` |
| `resident` | Warga / Citizen | `treasury:read`, `water:read` |

---

## 🛠️ Tech Stack & Architecture

- **Language:** Go (1.24+)
- **HTTP Framework:** [Gin Web Framework](https://gin-gonic.com/)
- **ORM:** [GORM](https://gorm.io/)
- **Database:** PostgreSQL
- **Security:** `golang.org/x/crypto/bcrypt`, `golang-jwt/jwt/v5`
- **Containerization & Orchestration:** Docker, Docker Compose, Kubernetes manifests (`k8s/`)

---

## 📁 Directory Structure

```text
.
├── Dockerfile              # Multi-stage production container build
├── docker-compose.yml      # Local development container stack
├── k8s/                    # Kubernetes deployment & service manifests
├── main.go                 # Router initialization, CORS, middleware & route definitions
├── internal/
│   ├── database/           # DB initialization, auto-migration & seed scripts
│   ├── handlers/           # Gin controllers for Auth, Water, Treasury & Announcements
│   ├── middleware/         # JWT validation & RBAC permission checks
│   └── models/             # GORM domain models (User, Role, Permission, Transaction, etc.)
└── test/                   # Automated API integration and unit tests
```

---

## 📦 Getting Started

### Local Setup

1. Configure environment variables (or rely on development defaults):
   ```bash
   export DB_HOST=localhost
   export DB_PORT=5432
   export DB_USER=postgres
   export DB_PASSWORD=postgres
   export DB_NAME=rukun_warga
   export JWT_SECRET=supersecretkey
   ```

2. Download dependencies and run the server:
   ```bash
   go mod tidy
   go run main.go
   ```

3. Server runs on `http://localhost:8080`.

### Running with Docker Compose

```bash
docker compose up -d
```

### Running Tests

```bash
go test -v ./...
```
