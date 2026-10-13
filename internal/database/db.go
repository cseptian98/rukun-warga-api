package database

import (
	"log"
	"os"
	"time"

	"github.com/cseptian/rukun-warga-api/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
	var err error

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost user=postgres password=password dbname=rukunwarga port=5432 sslmode=disable"
	}

	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	err = DB.AutoMigrate(
		&models.Neighbor{},
		&models.Reading{},
		&models.Permission{},
		&models.Role{},
		&models.User{},
		&models.Transaction{},
		&models.Announcement{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	if err := AutoMigrateAndSeed(DB); err != nil {
		log.Printf("Seeding warning: %v", err)
	}

	log.Println("Database connection established and migrated.")
}

func AutoMigrateAndSeed(db *gorm.DB) error {
	permissions := []models.Permission{
		{Code: "treasury:read", Description: "View treasury summary and transactions"},
		{Code: "treasury:write", Description: "Record and manage treasury transactions"},
		{Code: "water:manage", Description: "Manage water neighbors, readings, and payments"},
		{Code: "announcements:publish", Description: "Publish community announcements"},
		{Code: "water:read", Description: "View water readings and neighbors"},
	}

	permMap := make(map[string]models.Permission)
	for _, p := range permissions {
		var existing models.Permission
		if err := db.Where("code = ?", p.Code).First(&existing).Error; err != nil {
			existing = p
			if err := db.Create(&existing).Error; err != nil {
				return err
			}
		}
		permMap[p.Code] = existing
	}

	roleDefs := []struct {
		Name        string
		Description string
		PermCodes   []string
	}{
		{
			Name:        "admin",
			Description: "System Administrator",
			PermCodes:   []string{"treasury:read", "treasury:write", "water:manage", "announcements:publish", "water:read"},
		},
		{
			Name:        "treasurer",
			Description: "RW Treasurer",
			PermCodes:   []string{"treasury:read", "treasury:write", "water:read", "water:manage"},
		},
		{
			Name:        "resident",
			Description: "RW Resident",
			PermCodes:   []string{"treasury:read", "water:read"},
		},
	}

	roleMap := make(map[string]models.Role)
	for _, rd := range roleDefs {
		var role models.Role
		if err := db.Where("name = ?", rd.Name).First(&role).Error; err != nil {
			role = models.Role{
				Name:        rd.Name,
				Description: rd.Description,
			}
			if err := db.Create(&role).Error; err != nil {
				return err
			}
		}

		var perms []models.Permission
		for _, code := range rd.PermCodes {
			if p, ok := permMap[code]; ok {
				perms = append(perms, p)
			}
		}
		if err := db.Model(&role).Association("Permissions").Replace(perms); err != nil {
			return err
		}
		roleMap[rd.Name] = role
	}

	var arthur models.Neighbor
	var residentNeighborID *string
	if err := db.Where("name = ?", "Arthur Morgan").First(&arthur).Error; err != nil {
		arthur = models.Neighbor{
			Name:        "Arthur Morgan",
			Address:     "Block A No. 12",
			JoinDate:    time.Now(),
			TotalUnpaid: 0,
		}
		if err := db.Create(&arthur).Error; err == nil {
			residentNeighborID = &arthur.ID
		}
	} else {
		residentNeighborID = &arthur.ID
	}

	userDefs := []struct {
		Username   string
		Email      string
		Password   string
		FullName   string
		RoleName   string
		NeighborID *string
	}{
		{
			Username:   "admin",
			Email:      "admin@kampungmelati.id",
			Password:   "admin123",
			FullName:   "Admin Kampung Melati",
			RoleName:   "admin",
			NeighborID: nil,
		},
		{
			Username:   "treasurer",
			Email:      "treasurer@kampungmelati.id",
			Password:   "bendahara123",
			FullName:   "Bendahara Kampung Melati",
			RoleName:   "treasurer",
			NeighborID: nil,
		},
		{
			Username:   "resident",
			Email:      "resident@kampungmelati.id",
			Password:   "warga123",
			FullName:   "Arthur Morgan",
			RoleName:   "resident",
			NeighborID: residentNeighborID,
		},
	}

	for _, ud := range userDefs {
		var user models.User
		hash, err := bcrypt.GenerateFromPassword([]byte(ud.Password), 10)
		if err != nil {
			return err
		}

		if err := db.Where("email = ?", ud.Email).First(&user).Error; err != nil {
			user = models.User{
				Username:     ud.Username,
				Email:        ud.Email,
				PasswordHash: string(hash),
				FullName:     ud.FullName,
				NeighborID:   ud.NeighborID,
			}
			if err := db.Create(&user).Error; err != nil {
				return err
			}
		} else {
			user.PasswordHash = string(hash)
			if ud.NeighborID != nil {
				user.NeighborID = ud.NeighborID
			}
			db.Save(&user)
		}

		if role, ok := roleMap[ud.RoleName]; ok {
			if err := db.Model(&user).Association("Roles").Replace([]models.Role{role}); err != nil {
				return err
			}
		}
	}

	return nil
}
