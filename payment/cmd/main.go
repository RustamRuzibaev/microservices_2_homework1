package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	paymentV1 "github.com/RustamRuzibaev/microservices_2_homework1/shared/pkg/proto/payment/v1"
)

const grpcPort = 50052

// paymentService реализует gRPC сервис отвечающий за оплату заказов
type paymentService struct {
	paymentV1.UnimplementedPaymentServiceServer

	mu sync.RWMutex
}

// PayOrder обрабатывает команду на оплату и возвращает `transaction_uuid`.
func (s *paymentService) PayOrder(_ context.Context, req *paymentV1.PayOrderRequest) (*paymentV1.PayOrderResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("IN PAYORDER ")
	// Генерируем UUID для новой транзакции
	newTransactionUUID := uuid.NewString()

	log.Printf("Оплата прошла успешно transaction_uuid: %s", newTransactionUUID)

	return &paymentV1.PayOrderResponse{
		TransactionUuid: newTransactionUUID,
	}, nil
}

func main() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Printf("failed to listen: %v\n", err)
		return
	}
	defer func() {
		if cerr := lis.Close(); cerr != nil {
			log.Printf("failed to close listener: %v\n", cerr)
		}
	}()

	// Создаем gRPC сервер
	s := grpc.NewServer()

	// Регистрируем наш сервис
	service := &paymentService{
		mu: sync.RWMutex{},
	}

	paymentV1.RegisterPaymentServiceServer(s, service)

	// Включаем рефлексию для отладки
	reflection.Register(s)

	go func() {
		log.Printf("🚀 gRPC server listening on %d\n", grpcPort)
		err = s.Serve(lis)
		if err != nil {
			log.Printf("failed to serve: %v\n", err)
			return
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("🛑 Shutting down gRPC server...")

	s.GracefulStop()
	log.Println("✅ Server stopped")
}
