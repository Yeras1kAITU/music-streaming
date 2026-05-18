package handler

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/music-streaming/proto/user"
	"github.com/music-streaming/user-service/internal/domain"
	"github.com/music-streaming/user-service/internal/service"
)

type UserHandler struct {
	pb.UnimplementedUserServiceServer
	service *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{service: svc}
}

func (h *UserHandler) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	userID, err := h.service.Register(ctx, req.Email, req.Password, req.Username)
	if err != nil {
		if err == domain.ErrUserExists {
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.RegisterResponse{UserId: userID, Message: "User registered successfully"}, nil
}

func (h *UserHandler) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	token, refreshToken, err := h.service.Login(ctx, req.Email, req.Password)
	if err != nil {
		if err == domain.ErrInvalidCredentials {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		if err == domain.ErrEmailNotVerified {
			return nil, status.Error(codes.PermissionDenied, "email not verified")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.LoginResponse{Token: token, RefreshToken: refreshToken}, nil
}

func (h *UserHandler) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
	user, err := h.service.GetUser(ctx, req.UserId)
	if err != nil {
		if err == domain.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.User{
		Id:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt.Unix(),
	}, nil
}

func (h *UserHandler) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.User, error) {
	user, err := h.service.UpdateUser(ctx, req.UserId, req.Username, req.Email)
	if err != nil {
		if err == domain.ErrUserNotFound {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.User{
		Id:        user.ID,
		Email:     user.Email,
		Username:  user.Username,
		Role:      user.Role,
		Verified:  user.Verified,
		CreatedAt: user.CreatedAt.Unix(),
	}, nil
}

func (h *UserHandler) DeleteUser(ctx context.Context, req *pb.DeleteUserRequest) (*pb.DeleteUserResponse, error) {
	if err := h.service.DeleteUser(ctx, req.UserId); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.DeleteUserResponse{Message: "User deleted successfully"}, nil
}

func (h *UserHandler) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	userID, valid := h.service.ValidateToken(ctx, req.Token)
	return &pb.ValidateTokenResponse{UserId: userID, Valid: valid}, nil
}

func (h *UserHandler) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if err := h.service.Logout(ctx, req.Token); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.LogoutResponse{Message: "Logged out successfully"}, nil
}

func (h *UserHandler) ChangePassword(ctx context.Context, req *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error) {
	if err := h.service.ChangePassword(ctx, req.UserId, req.OldPassword, req.NewPassword); err != nil {
		if err == domain.ErrInvalidPassword {
			return nil, status.Error(codes.Unauthenticated, "invalid old password")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ChangePasswordResponse{Message: "Password changed successfully"}, nil
}

func (h *UserHandler) VerifyEmail(ctx context.Context, req *pb.VerifyEmailRequest) (*pb.VerifyEmailResponse, error) {
	if err := h.service.VerifyEmail(ctx, req.UserId, req.Token); err != nil {
		return &pb.VerifyEmailResponse{Success: false, Message: err.Error()}, nil
	}
	return &pb.VerifyEmailResponse{Success: true, Message: "Email verified successfully"}, nil
}

func (h *UserHandler) ForgotPassword(ctx context.Context, req *pb.ForgotPasswordRequest) (*pb.ForgotPasswordResponse, error) {
	if err := h.service.ForgotPassword(ctx, req.Email); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.ForgotPasswordResponse{Message: "If the email exists, a reset link will be sent"}, nil
}

func (h *UserHandler) ResetPassword(ctx context.Context, req *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error) {
	if err := h.service.ResetPassword(ctx, req.Token, req.NewPassword); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid or expired reset token")
	}
	return &pb.ResetPasswordResponse{Message: "Password reset successfully"}, nil
}

func (h *UserHandler) GetUserStats(ctx context.Context, req *pb.GetUserStatsRequest) (*pb.UserStats, error) {
	stats, err := h.service.GetUserStats(ctx, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.UserStats{
		TotalPlaylists:       stats["total_playlists"].(int32),
		TotalTracksUploaded:  stats["total_tracks_uploaded"].(int32),
		TotalPlays:           stats["total_plays"].(int32),
		SubscriptionDaysLeft: stats["subscription_days_left"].(int64),
	}, nil
}
