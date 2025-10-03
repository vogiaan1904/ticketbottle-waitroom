package grpc

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-waitroom/internal/service"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	waitroompb "github.com/vogiaan1904/ticketbottle-waitroom/protogen/waitroom"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	return nil, status.Errorf(codes.Unimplemented, "method JoinQueue not implemented")
}
