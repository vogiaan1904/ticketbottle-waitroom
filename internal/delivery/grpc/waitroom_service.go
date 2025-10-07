package grpc

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	resp "github.com/vogiaan1904/ticketbottle-waitroom/pkg/response"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/util"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
)

type WaitroomGrpcService struct {
	svc service.WaitroomService
	l   logger.Logger
	waitroompb.UnimplementedWaitroomServiceServer
}

func NewWaitroomGrpcService(svc service.WaitroomService, l logger.Logger) waitroompb.WaitroomServiceServer {
	return &WaitroomGrpcService{
		svc: svc,
		l:   l,
	}
}

func (s *WaitroomGrpcService) JoinQueue(ctx context.Context, req *waitroompb.JoinQueueRequest) (*waitroompb.JoinQueueResponse, error) {
	input := service.JoinQueueInput{
		UserID:    req.UserId,
		EventID:   req.EventId,
		UserAgent: req.UserAgent,
		IPAddress: req.IpAddress,
	}

	out, err := s.svc.JoinQueue(ctx, &input)
	if err != nil {
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.JoinQueueResponse{
		SessionId:    out.SessionID,
		Position:     out.Position,
		QueueLength:  out.QueueLength,
		QueuedAt:     util.TimeToISO8601Str(out.QueuedAt),
		ExpiresAt:    util.TimeToISO8601Str(out.ExpiresAt),
		WebsocketUrl: out.WebSocketURL,
	}, nil
}

func (s *WaitroomGrpcService) GetQueueStatus(ctx context.Context, req *waitroompb.GetQueueStatusRequest) (*waitroompb.QueueStatusResponse, error) {
	out, err := s.svc.GetQueueStatus(ctx, req.SessionId)
	if err != nil {
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.QueueStatusResponse{
		SessionId:     out.SessionID,
		Position:      out.Position,
		QueueLength:   out.QueueLength,
		QueuedAt:      util.TimeToISO8601Str(out.QueuedAt),
		ExpiresAt:     util.TimeToISO8601Str(out.ExpiresAt),
		CheckoutToken: out.CheckoutToken,
		CheckoutUrl:   out.CheckoutURL,
	}, nil
}

func (s *WaitroomGrpcService) LeaveQueue(ctx context.Context, req *waitroompb.LeaveQueueRequest) (*waitroompb.LeaveQueueResponse, error) {
	err := s.svc.LeaveQueue(ctx, req.SessionId)
	if err != nil {
		return nil, resp.ParseGRPCError(err)
	}

	return &waitroompb.LeaveQueueResponse{
		SessionId: req.SessionId,
		Message:   "Queue left successfully",
	}, nil
}
