// InfoMark - a platform for managing courses with
//            distributing exercise sheets and testing exercise submissions
// Copyright (C) 2019  ComputerGraphics Tuebingen
// Authors: Patrick Wieschollek
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package api

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/cgtuebingen/infomark-backend/api/app"
	"github.com/cgtuebingen/infomark-backend/email"
	"github.com/cgtuebingen/infomark-backend/logging"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/viper"
)

// Server provides an http.Server.
type Server struct {
	*http.Server
}

// NewServer creates and configures an APIServer serving all application routes.
func NewServer() (*Server, error) {
	log.Println("configuring server...")
	logger := logging.NewLogger()

	// db, err := sqlx.Connect("sqlite3", "__deleteme.db")
	db, err := sqlx.Connect("postgres", viper.GetString("database_connection"))
	// db, err := sqlx.Connect("postgres", "user=postgres dbname=infomark password=postgres sslmode=disable")
	if err != nil {
		logger.WithField("module", "database").Error(err)
		return nil, err
	}

	apiHandler, err := app.New(db, true)
	if err != nil {
		return nil, err
	}

	var addr string
	port := viper.GetString("port")

	// allow port to be set as localhost:3000 in env during development to avoid "accept incoming network connection" request on restarts
	if strings.Contains(port, ":") {
		addr = port
	} else {
		addr = ":" + port
	}

	srv := http.Server{
		Addr:           addr,
		Handler:        apiHandler,
		ReadTimeout:    time.Duration(viper.GetInt64("server_read_timeout_sec")) * time.Second,
		WriteTimeout:   time.Duration(viper.GetInt64("server_write_timeout_sec")) * time.Second,
		MaxHeaderBytes: viper.GetInt("server_write_timeout_sec"),
	}

	return &Server{&srv}, nil
}

// Start runs ListenAndServe on the http.Server with graceful shutdown.
func (srv *Server) Start() {

	log.Println("starting server...")
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			panic(err)
		}
	}()
	log.Printf("Listening on %s\n", srv.Addr)

	log.Println("starting background email sender...")
	go email.BackgroundSend(email.OutgoingEmailsChannel)

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	sig := <-quit
	log.Println("Shutting down server... Reason:", sig)
	// teardown logic...

	if err := srv.Shutdown(context.Background()); err != nil {
		panic(err)
	}
	log.Println("Server gracefully stopped")
}

// func SendEmailWorker(
// 	recipients <-chan User,
// 	subject string,
// 	body string,
// 	sender User,
// ) {
// 	fmt.Println("Register the worker")
// 	for _, recipient := range recipients {
// 		// add sender identity
// 		// msg := email.NewEmailFromUser(
// 		// 	recipient.Email,
// 		// 	data.Subject,
// 		// 	data.Body,
// 		// 	accessUser,
// 		// )

// 		// if err := email.DefaultMail.Send(msg); err != nil {
// 		// 	render.Render(w, r, ErrInternalServerErrorWithDetails(err))
// 		// 	return
// 		// }
// 		fmt.Println("send email to", recipient)
// 	}
// }
