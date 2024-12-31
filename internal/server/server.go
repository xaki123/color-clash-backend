package server 

import (
	"net/http"
	"log"
	"colback/internal/ws"
	"colback/internal/router"
)

func Start(addr string){
	hub := ws.NewHub()
	go hub.Run()
	
	r := router.New(hub)

	srv := &http.Server{
		Addr:addr,
		Handler:r,
	}
	
	go log.Println("server running on: ",addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal("cant start the server: ",err)
	}
}