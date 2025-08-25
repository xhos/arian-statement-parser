package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"

	"arian-statement-parser/internal/domain"
	pb "arian-statement-parser/internal/gen/arian/v1"

	"github.com/charmbracelet/log"
	money "google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Client struct {
	conn          *grpc.ClientConn
	accountClient pb.AccountServiceClient
	txClient      pb.TransactionServiceClient
	userClient    pb.UserServiceClient
	authToken     string
	log           *log.Logger
}

func NewClient(arianURL, _, authToken string) (*Client, error) {
	// Use TLS credentials for port 443, insecure for others
	var creds credentials.TransportCredentials
	if arianURL[len(arianURL)-4:] == ":443" {
		creds = credentials.NewTLS(&tls.Config{})
	} else {
		creds = insecure.NewCredentials()
	}
	
	conn, err := grpc.NewClient(arianURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	return &Client{
		conn:          conn,
		accountClient: pb.NewAccountServiceClient(conn),
		txClient:      pb.NewTransactionServiceClient(conn),
		userClient:    pb.NewUserServiceClient(conn),
		authToken:     authToken,
		log:           log.NewWithOptions(os.Stderr, log.Options{Prefix: "grpc-client"}),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// GetUser retrieves a user by UUID
func (c *Client) GetUser(userUUID string) (*pb.User, error) {
	ctx := c.withAuth(context.Background())

	req := &pb.GetUserRequest{
		Id: userUUID,
	}

	resp, err := c.userClient.GetUser(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	c.log.Info("successfully fetched user", "user_id", userUUID)
	return resp.User, nil
}

func (c *Client) GetAccounts(userID string) ([]*pb.Account, error) {
	ctx := c.withAuth(context.Background())

	req := &pb.ListAccountsRequest{
		UserId: userID,
	}

	resp, err := c.accountClient.ListAccounts(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	c.log.Info("successfully fetched accounts", "count", len(resp.Accounts))
	return resp.Accounts, nil
}

func (c *Client) CreateTransaction(userID string, tx *domain.Transaction) error {
	ctx := c.withAuth(context.Background())

	// convert domain transaction to gRPC request
	req := &pb.CreateTransactionRequest{
		UserId:    userID,
		AccountId: int64(tx.AccountID),
		TxDate:    timestamppb.New(tx.TxDate),
		TxAmount: &money.Money{
			CurrencyCode: tx.TxCurrency,
			Units:        int64(tx.TxAmount),
			Nanos:        int32((tx.TxAmount - float64(int64(tx.TxAmount))) * 1e9),
		},
		Direction: c.convertDirection(tx.TxDirection),
	}

	// Optional fields
	if tx.TxDesc != "" {
		req.Description = &tx.TxDesc
	}
	if tx.Merchant != "" {
		req.Merchant = &tx.Merchant
	}
	if tx.UserNotes != "" {
		req.UserNotes = &tx.UserNotes
	}

	resp, err := c.txClient.CreateTransaction(ctx, req)
	if err != nil {
		// check for duplicate transaction (conflict)
		if grpcStatus := status.Code(err); grpcStatus == codes.AlreadyExists {
			c.log.Info("skipping duplicate transaction", "email_id", tx.EmailID)
			return nil // not a fatal error, just a duplicate
		}
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	c.log.Info("transaction created successfully", "email_id", tx.EmailID, "tx_id", resp.Transaction.Id)
	return nil
}

// withAuth adds authentication metadata to the context
func (c *Client) withAuth(ctx context.Context) context.Context {
	md := metadata.Pairs("x-internal-key", c.authToken)
	return metadata.NewOutgoingContext(ctx, md)
}

// convertDirection converts domain Direction to gRPC TransactionDirection
func (c *Client) convertDirection(dir domain.Direction) pb.TransactionDirection {
	switch dir {
	case domain.In:
		return pb.TransactionDirection_DIRECTION_INCOMING
	case domain.Out:
		return pb.TransactionDirection_DIRECTION_OUTGOING
	default:
		return pb.TransactionDirection_DIRECTION_UNSPECIFIED
	}
}