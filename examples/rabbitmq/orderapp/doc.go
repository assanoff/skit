// Package orderapp is the domain of the orders service in the broker
// example. It knows the contract of package broker — a broker.Publisher for
// the events it sends and a broker.Handler for the ones it consumes — and
// never the transport: RabbitMQ appears only in the assembly of package main.
//
// # Usage
//
//	app := orderapp.New(log, pub) // pub: *rabbitmq.Publisher as broker.Publisher
//	err := app.Create(ctx, orderapp.Order{ID: "o-1", Total: 1500})
//
// The consumer of the payments takes app.HandlePaid as its broker.Handler.
package orderapp
