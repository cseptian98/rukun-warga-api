package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Neighbor struct {
	ID                string     `gorm:"primaryKey" json:"id"`
	Name              string     `json:"name"`
	Address           string     `json:"address"`
	JoinDate          time.Time  `json:"join_date"`
	LastReadingDate   *time.Time `json:"last_reading_date"`
	LastReadingVolume float64    `json:"last_reading_volume"`
	TotalUnpaid       float64    `json:"total_unpaid"`
	Readings          []Reading  `gorm:"foreignKey:NeighborID" json:"readings"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Reading struct {
	ID         string    `gorm:"primaryKey" json:"id"`
	NeighborID string    `json:"neighbor_id"`
	Date       time.Time `json:"date"`
	Volume     float64   `json:"volume"`
	TotalCost  float64   `json:"total_cost"`
	IsPaid     bool      `json:"is_paid"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Permission struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	Code        string    `gorm:"uniqueIndex;not null" json:"code"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Role struct {
	ID          string       `gorm:"primaryKey" json:"id"`
	Name        string       `gorm:"uniqueIndex;not null" json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `gorm:"many2many:role_permissions;" json:"permissions"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type User struct {
	ID           string    `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"not null" json:"-"`
	FullName     string    `json:"full_name"`
	NeighborID   *string   `json:"neighbor_id"`
	Roles        []Role    `gorm:"many2many:user_roles;" json:"roles"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Transaction struct {
	ID                string    `gorm:"primaryKey" json:"id"`
	TransactionNumber string    `gorm:"uniqueIndex;not null" json:"transaction_number"`
	Type              string    `gorm:"not null" json:"type"`      // dues, water_billing, expenditure, other_income
	Direction         string    `gorm:"not null" json:"direction"` // income, expense
	Category          string    `json:"category"`
	Amount            float64   `gorm:"not null" json:"amount"`
	Status            string    `gorm:"not null;default:'completed'" json:"status"` // completed, pending, cancelled
	Period            string    `json:"period"`
	PaymentMethod     string    `json:"payment_method"`
	Description       string    `json:"description"`
	Date              time.Time `json:"date"`
	UserID            *string   `json:"user_id"`
	NeighborID        *string   `json:"neighbor_id"`
	ReadingID         *string   `json:"reading_id"`
	RecordedByID      string    `json:"recorded_by_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Announcement struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"not null" json:"title"`
	Content   string    `gorm:"not null" json:"content"`
	Category  string    `json:"category"`
	Author    string    `json:"author"`
	Priority  string    `json:"priority"` // Normal, High
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate hooks for UUID generation
func (n *Neighbor) BeforeCreate(tx *gorm.DB) (err error) {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return
}

func (r *Reading) BeforeCreate(tx *gorm.DB) (err error) {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return
}

func (p *Permission) BeforeCreate(tx *gorm.DB) (err error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	return
}

func (r *Role) BeforeCreate(tx *gorm.DB) (err error) {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	return
}

func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return
}

func (t *Transaction) BeforeCreate(tx *gorm.DB) (err error) {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return
}

func (a *Announcement) BeforeCreate(tx *gorm.DB) (err error) {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return
}
