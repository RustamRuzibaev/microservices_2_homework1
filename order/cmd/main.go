package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	//	"github.com/brianvoe/gofakeit/v7"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	google_uuid "github.com/google/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	//"google.golang.org/protobuf/types/known/timestamppb"
	//"google.golang.org/protobuf/types/known/wrapperspb"

	orderV1 "github.com/RustamRuzibaev/microservices_2_homework1/shared/pkg/openapi/order/v1"
	inventoryV1 "github.com/RustamRuzibaev/microservices_2_homework1/shared/pkg/proto/inventory/v1"
	paymentV1 "github.com/RustamRuzibaev/microservices_2_homework1/shared/pkg/proto/payment/v1"
)

const (
	httpPort     = "8080"
	serverURL    = "http://localhost:8080"
	inventoryURL = "localhost:50051"
	paymentURL   = "localhost:50052"
	// Таймауты для HTTP-сервера
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// OrderStorage представляет потокобезопасное хранилище данных о погоде
type OrderStorage struct {
	mu     sync.RWMutex
	orders map[string]*orderV1.OrderDto
}

// NewOrderStorage создает новое хранилище данных о погоде
func NewOrderStorage() *OrderStorage {
	return &OrderStorage{
		orders: make(map[string]*orderV1.OrderDto),
	}
}

// GetOrder возвращает информацию о заказе по Uuid
func (s *OrderStorage) GetOrder(uuid string) *orderV1.OrderDto {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[uuid]
	if !ok {
		return nil
	}

	return order
}

// // UpdateOrder обновляет данные о заказе по заданному uuid
func (s *OrderStorage) UpdateOrder(uuid string, order *orderV1.OrderDto) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.orders[uuid] = order
}

// OrderHandler реализует интерфейс orderV1.Handler для обработки запросов к API заказов
type OrderHandler struct {
	storage *OrderStorage
}

// NewOrderHandler создает новый обработчик запросов к API заказов
func NewOrderHandler(storage *OrderStorage) *OrderHandler {
	return &OrderHandler{
		storage: storage,
	}
}

func (h *OrderHandler) CreateOrder(ctx context.Context, req *orderV1.CreateOrderRequest) (orderV1.CreateOrderRes, error) {
	//Инициализация клиента inventoryService

	conn, err := grpc.NewClient(
		inventoryURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("failed to connect: %v\n", err)
		return &orderV1.InternalServerError{
			Code:    500,
			Message: "URL for inventory service '" + inventoryURL + "' is unreachable",
		}, nil
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("failed to close connect: %v", cerr)
		}
	}()

	// Создаем gRPC клиент
	client := inventoryV1.NewInventoryServiceClient(conn)

	uuids := req.GetPartUuids()
	stringUuids := []string{}
	for _, uuid := range uuids {
		stringUuids = append(stringUuids, uuid.String())
	}
	filter := &inventoryV1.PartsFilter{
		Uuids: stringUuids,
	}
	inventoryRes, err := client.ListParts(ctx, &inventoryV1.ListPartsRequest{
		Filter: filter,
	})

	if err != nil {
		log.Printf("Ошибка при вызове InventoryService: %v", err)
		return &orderV1.InternalServerError{
			Code:    500,
			Message: "Inventory service error: " + err.Error(),
		}, nil
	}

	// if err != nil {
	// 	log.Printf("Ошибка при создании заказа: %v", req)
	// }

	if len(inventoryRes.Parts) != len(uuids) {
		return &orderV1.BadRequestError{
			Code:    400,
			Message: fmt.Sprintf("Not all parts from %v has been found", uuids),
		}, nil
	}
	var totalPrice float64
	for _, part := range inventoryRes.Parts {
		totalPrice = totalPrice + part.GetPrice()
	}

	order := &orderV1.OrderDto{
		OrderUUID:  google_uuid.New(),
		UserUUID:   req.UserUUID.String(),
		PartUuids:  uuids,
		TotalPrice: float32(totalPrice),
		Status:     "PENDING_PAYMENT",
	}
	h.storage.mu.Lock()
	h.storage.orders[order.OrderUUID.String()] = order
	h.storage.mu.Unlock()

	return &orderV1.CreateOrderResponse{
		OrderUUID:  order.OrderUUID,
		TotalPrice: order.TotalPrice,
	}, nil

}

func (h *OrderHandler) PayOrder(ctx context.Context, req *orderV1.PayOrderRequest, params orderV1.PayOrderParams) (orderV1.PayOrderRes, error) {
	//Инициализация клиента paymentService

	conn, err := grpc.NewClient(
		paymentURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("failed to connect: %v\n", err)
		return &orderV1.InternalServerError{
			Code:    500,
			Message: "URL for payment service '" + paymentURL + "' is unreachable",
		}, nil
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("failed to close connect: %v", cerr)
		}
	}()

	// Создаем gRPC клиент
	client := paymentV1.NewPaymentServiceClient(conn)
	h.storage.mu.Lock()
	defer h.storage.mu.Unlock()
	orderToPay, ok := h.storage.orders[params.OrderUUID]
	if !ok {
		return &orderV1.NotFoundError{
			Code:    404,
			Message: fmt.Sprintf("Order %v not found", params.OrderUUID),
		}, nil
	}

	paymentMethods := map[string]paymentV1.PaymentMethod{
		"UNKNOWN":        paymentV1.PaymentMethod_PAYMENT_METHOD_UNKNOWN,
		"CARD":           paymentV1.PaymentMethod_PAYMENT_METHOD_CARD,
		"SBP":            paymentV1.PaymentMethod_PAYMENT_METHOD_SBP,
		"CREDIT_CARD":    paymentV1.PaymentMethod_PAYMENT_METHOD_CREDIT_CARD,
		"INVESTOR_MONEY": paymentV1.PaymentMethod_PAYMENT_METHOD_INVESTOR_MONEY,
	}

	paymentRes, err := client.PayOrder(ctx, &paymentV1.PayOrderRequest{
		OrderUuid:     orderToPay.OrderUUID.String(),
		UserUuid:      orderToPay.UserUUID,
		PaymentMethod: paymentMethods[req.PaymentMethod],
	})
	if err != nil {
		log.Printf("Ошибка при вызове PaymentService: %v", err)
		return &orderV1.InternalServerError{
			Code:    500,
			Message: "Payment service unavailable: " + err.Error(),
		}, nil
	}

	payTransUUID, err := google_uuid.Parse(paymentRes.TransactionUuid)
	if err != nil {
		//log.Fatalf("Error:%v cannot make uuid out of %v", err, paymentRes.TransactionUuid)
		log.Printf("Ошибка при вызове : google_uuid.Parse()%v", err)
		return &orderV1.InternalServerError{
			Code:    500,
			Message: "PayOrder error: " + err.Error(),
		}, nil
	}

	orderToPay.TransactionUUID = orderV1.NewOptUUID(payTransUUID)
	orderToPay.PaymentMethod = orderV1.NewOptNilPaymentMethod(orderV1.PaymentMethod(req.PaymentMethod))
	orderToPay.Status = orderV1.OrderStatusPAID

	return &orderV1.PayOrderResponse{
		TransactionUUID: payTransUUID,
	}, nil

}

// GetOrderByUuid обрабатывает запрос на получение данных о заказах по uuid
func (h *OrderHandler) GetOrderByUuid(_ context.Context, params orderV1.GetOrderByUuidParams) (orderV1.GetOrderByUuidRes, error) {
	order := h.storage.GetOrder(params.OrderUUID)
	if order == nil {
		return &orderV1.NotFoundError{
			Code:    404,
			Message: "Order for uuid '" + params.OrderUUID + "' not found",
		}, nil
	}

	return order, nil
}

// // UpdateOrderByUuid обрабатывает запрос на обновление данных о погоде по названию города
// func (h *OrderHandler) UpdateOrderByUuid(_ context.Context, req *orderV1.UpdateOrderRequest, params orderV1.UpdateOrderByUuidParams) (orderV1.UpdateOrderByUuidRes, error) {
// 	// Создаем объект погоды с полученными данными
// 	order := &orderV1.Order{
// 		Uuid:        params.Uuid,
// 		Temperature: req.Temperature,
// 		UpdatedAt:   time.Now(),
// 	}

// 	// Обновляем данные в хранилище
// 	h.storage.UpdateOrder(params.Uuid, order)

// 	return order, nil
// }

// CancelOrder Отменяет заказ по uuid
func (h *OrderHandler) CancelOrder(_ context.Context, params orderV1.CancelOrderParams) (orderV1.CancelOrderRes, error) {

	order := h.storage.GetOrder(params.OrderUUID)
	if order == nil {
		return &orderV1.NotFoundError{
			Code:    404,
			Message: "Order for uuid '" + params.OrderUUID + "' not found",
		}, nil
	} else if order.GetStatus() == "PAID" {
		return &orderV1.ConflictError{
			Code:    409,
			Message: "Order for uuid '" + params.OrderUUID + "' has been paid already and cannot be cancelled",
		}, nil
	}

	// Обновляем данные в хранилище
	order.Status = "CANCELLED"
	h.storage.UpdateOrder(params.OrderUUID, order)

	return &orderV1.CancelOrderNoContent{
		Code:    204,
		Message: "Order for uuid '" + params.OrderUUID + "' has been cancelled successfully",
	}, nil
}

// NewError создает новую ошибку в формате GenericError
func (h *OrderHandler) NewError(_ context.Context, err error) *orderV1.GenericErrorStatusCode {
	return &orderV1.GenericErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response: orderV1.GenericError{
			Code:    orderV1.NewOptInt(http.StatusInternalServerError),
			Message: orderV1.NewOptString(err.Error()),
		},
	}
}

func main() {
	// Создаем хранилище для данных о погоде
	storage := NewOrderStorage()

	// Создаем обработчик API погоды
	orderHandler := NewOrderHandler(storage)

	// Создаем OpenAPI сервер
	orderServer, err := orderV1.NewServer(orderHandler)
	if err != nil {
		log.Fatalf("ошибка создания сервера OpenAPI: %v", err)
	}

	// Инициализируем роутер Chi
	r := chi.NewRouter()

	// Добавляем middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Second))
	//	r.Use(customMiddleware.RequestLogger)

	// Монтируем обработчики OpenAPI
	r.Mount("/", orderServer)

	// Запускаем HTTP-сервер
	server := &http.Server{
		Addr:              net.JoinHostPort("localhost", httpPort),
		Handler:           r,
		ReadHeaderTimeout: readHeaderTimeout, // Защита от Slowloris атак - тип DDoS-атаки, при которой
		// атакующий умышленно медленно отправляет HTTP-заголовки, удерживая соединения открытыми и истощая
		// пул доступных соединений на сервере. ReadHeaderTimeout принудительно закрывает соединение,
		// если клиент не успел отправить все заголовки за отведенное время.
	}

	// Запускаем сервер в отдельной горутине
	go func() {
		log.Printf("🚀 HTTP-сервер запущен на порту %s\n", httpPort)
		err = server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("❌ Ошибка запуска сервера: %v\n", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Завершение работы сервера...")

	// Создаем контекст с таймаутом для остановки сервера
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	err = server.Shutdown(ctx)
	if err != nil {
		log.Printf("❌ Ошибка при остановке сервера: %v\n", err)
	}

	log.Println("✅ Сервер остановлен")
}
