package main

import (
	"fmt"
	"log"
	"net"

	grpc "google.golang.org/grpc"

	pb "github.com/akhenakh/gomodem/gen/modemsvc/v1"
	modemsrv "github.com/akhenakh/gomodem/server"
)

func main() {
	port := 50051
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	modemServer := modemsrv.NewModemServer()

	pb.RegisterModemServiceServer(grpcServer, modemServer)

	fmt.Printf("Modem gRPC server listening on port %d...\n", port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
