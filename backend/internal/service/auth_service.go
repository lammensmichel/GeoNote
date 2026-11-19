package service

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/mimi6060/GeoNote/backend/internal/model"
	"github.com/mimi6060/GeoNote/backend/internal/repository"
)

// UserStore is the subset of the user repository used by AuthService.
// *repository.UserRepo implements it; tests use an in-memory fake.
type UserStore interface {
	Create(ctx context.Context, user *model.User) error
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetByID(ctx context.Context, id string) (*model.User, error)
	GetPasswordHash(ctx context.Context, id string) (string, error)
	Delete(ctx context.Context, id string) error
}

// Errors returned by DeleteAccount.
var (
	ErrWrongPassword = errors.New("mot de passe incorrect")
	ErrUserNotFound  = errors.New("utilisateur introuvable")
)

type AuthService struct {
	userRepo         UserStore
	jwtSecret        []byte
	jwtExpiry        time.Duration
	onAccountDeleted func(ctx context.Context)
}

func NewAuthService(userRepo UserStore, secret string, expiry time.Duration) *AuthService {
	return &AuthService{
		userRepo:  userRepo,
		jwtSecret: []byte(secret),
		jwtExpiry: expiry,
	}
}

func (s *AuthService) Register(ctx context.Context, req model.RegisterRequest) (*model.AuthResponse, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hash),
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	token, err := s.generateToken(user.ID)
	if err != nil {
		return nil, err
	}

	return &model.AuthResponse{Token: token, User: *user}, nil
}

func (s *AuthService) Login(ctx context.Context, req model.LoginRequest) (*model.AuthResponse, error) {
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, errors.New("identifiants invalides")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, errors.New("identifiants invalides")
	}

	token, err := s.generateToken(user.ID)
	if err != nil {
		return nil, err
	}

	return &model.AuthResponse{Token: token, User: *user}, nil
}

func (s *AuthService) ValidateToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("methode de signature invalide")
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", errors.New("token invalide")
	}

	userID, ok := claims["sub"].(string)
	if !ok {
		return "", errors.New("token invalide: sub manquant")
	}

	return userID, nil
}

func (s *AuthService) GetUser(ctx context.Context, id string) (*model.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

// OnAccountDeleted registers a callback run after a successful account
// deletion (used to invalidate the nearby-messages cache).
func (s *AuthService) OnAccountDeleted(fn func(ctx context.Context)) {
	s.onAccountDeleted = fn
}

// DeleteAccount permanently deletes the user and all their data, after
// checking the password again so a stolen or forgotten-open session
// cannot erase an account on its own.
func (s *AuthService) DeleteAccount(ctx context.Context, userID, password string) error {
	hash, err := s.userRepo.GetPasswordHash(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrWrongPassword
	}
	if err := s.userRepo.Delete(ctx, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if s.onAccountDeleted != nil {
		s.onAccountDeleted(ctx)
	}
	return nil
}

func (s *AuthService) generateToken(userID string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(s.jwtExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}
