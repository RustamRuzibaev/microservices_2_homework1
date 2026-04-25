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
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	inventoryV1 "github.com/RustamRuzibaev/microservices_2_homework1/shared/pkg/proto/inventory/v1"
)

const grpcPort = 50051

// inventoryService реализует gRPC сервис отвечающий за хранение
//
//	и предоставление информации о деталях для сборки космических кораблей.
type inventoryService struct {
	inventoryV1.UnimplementedInventoryServiceServer

	mu    sync.RWMutex
	parts map[string]*inventoryV1.Part
}

// AddPart создает новое наблюдение НЛО
func (s *inventoryService) AddPart(_ context.Context, req *inventoryV1.AddPartRequest) (*inventoryV1.AddPartResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Генерируем UUID для нового наблюдения
	newUUID := uuid.NewString()

	part := &inventoryV1.Part{
		Uuid:          newUUID,
		Name:          req.Part.GetName(),
		Description:   req.Part.GetDescription(),
		Price:         req.Part.GetPrice(),
		StockQuantity: req.Part.GetStockQuantity(),
		Category:      req.Part.GetCategory(),
		Dimensions:    req.Part.GetDimensions(),
		Manufacturer:  req.Part.GetManufacturer(),
		Tags:          req.Part.GetTags(),
		Metadata:      req.Part.GetMetadata(),
		CreatedAt:     timestamppb.New(time.Now()),
		UpdatedAt:     timestamppb.New(time.Now()),
	}

	s.parts[newUUID] = part

	log.Printf("Добавлена деталь с UUID %s", newUUID)

	return &inventoryV1.AddPartResponse{
		Uuid: newUUID,
	}, nil
}

// Get возвращает информацию о детали по UUID
func (s *inventoryService) GetPart(_ context.Context, req *inventoryV1.GetPartRequest) (*inventoryV1.GetPartResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	part, ok := s.parts[req.GetUuid()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "part with UUID %s not found", req.GetUuid())
	}

	return &inventoryV1.GetPartResponse{
		Part: part,
	}, nil
}

// // Update обновляет существующее наблюдение НЛО
// func (s *inventoryService) Update(_ context.Context, req *inventoryV1.UpdateRequest) (*emptypb.Empty, error) {
// 	s.mu.Lock()
// 	defer s.mu.Unlock()

// 	part, ok := s.parts[req.GetUuid()]
// 	if !ok {
// 		return nil, status.Errorf(codes.NotFound, "part with UUID %s not found", req.GetUuid())
// 	}

// 	if req.UpdateInfo == nil {
// 		return nil, status.Error(codes.InvalidArgument, "update_info cannot be nil")
// 	}

// 	// Обновляем поля, только если они были установлены в запросе
// 	if req.GetUpdateInfo().ObservedAt != nil {
// 		part.Info.ObservedAt = req.GetUpdateInfo().ObservedAt
// 	}

// 	if req.GetUpdateInfo().Location != nil {
// 		part.Info.Location = req.GetUpdateInfo().Location.Value
// 	}

// 	if req.GetUpdateInfo().Description != nil {
// 		part.Info.Description = req.GetUpdateInfo().Description.Value
// 	}

// 	if req.GetUpdateInfo().Color != nil {
// 		part.Info.Color = req.GetUpdateInfo().Color
// 	}

// 	if req.GetUpdateInfo().Sound != nil {
// 		part.Info.Sound = req.GetUpdateInfo().Sound
// 	}

// 	if req.GetUpdateInfo().DurationSeconds != nil {
// 		part.Info.DurationSeconds = req.GetUpdateInfo().DurationSeconds
// 	}

// 	part.UpdatedAt = timestamppb.New(time.Now())

// 	return &emptypb.Empty{}, nil
// }

// Delete удаляет запись о детали
func (s *inventoryService) DeletePart(_ context.Context, req *inventoryV1.DeletePartRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	part, ok := s.parts[req.GetUuid()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "part with UUID %s not found", req.GetUuid())
	}

	// Мягкое удаление - устанавливаем deleted_at
	//part.DeletedAt = timestamppb.New(time.Now())
	// Удаление из мапы по ключу (Uuid)
	delete(s.parts, part.Uuid)

	return &emptypb.Empty{}, nil
}

func (s *inventoryService) ListParts(_ context.Context, req *inventoryV1.ListPartsRequest) (*inventoryV1.ListPartsResponse, error) {
	//TODO:
	// - `ListParts`:
	//     - Если все поля фильтра пусты — возвращаются все детали.
	//     - Фильтрация происходит по принципу:
	//         - *логическое ИЛИ внутри одного поля фильтра* (например, имя `"main"` **или** `"main booster"`)
	//         - *логическое И между различными полями* (например, категория = `ENGINE` **и** страна = `"Germany"`)
	//     - Допустимо реализовать фильтрацию через **несколько последовательных проходов**:
	//         - сначала по UUID,
	//         - затем по имени,
	//         - затем по категории,
	//         - затем по странам производителей,
	//         - затем по тегам.

	log.Printf("IN ListParts")

	// слайс возвращаемых деталей
	parts := make([]*inventoryV1.Part, 0, len(s.parts))
	if len(req.Filter.GetUuids()) == 0 &&
		len(req.Filter.GetNames()) == 0 &&
		len(req.Filter.GetCategories()) == 0 &&
		len(req.Filter.GetManufacturerCountries()) == 0 &&
		len(req.Filter.GetTags()) == 0 {
		for _, part := range s.parts {
			parts = append(parts, part)
		}
	} else if len(req.Filter.GetUuids()) != 0 {
		log.Println("IN FOUND UUID")
		partsUuids := req.Filter.GetUuids()
		for _, partUuid := range partsUuids {
			if part, ok := s.parts[partUuid]; !ok {
				return nil, status.Errorf(codes.NotFound, "Search didn't found part with uuid %v", partUuid)

			} else {
				parts = append(parts, part)
			}
		}

	} else {
		log.Println("IN UNIMPLEMENTED")
		return nil, status.Errorf(codes.Unimplemented, "Full search is still not implented for filter %v", req.Filter.String())
	}
	return &inventoryV1.ListPartsResponse{
		Parts: parts,
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
	service := &inventoryService{
		mu:    sync.RWMutex{},
		parts: make(map[string]*inventoryV1.Part),
	}

	inventoryV1.RegisterInventoryServiceServer(s, service)

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
