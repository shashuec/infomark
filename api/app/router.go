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

package app

import (
  "net/http"
  "os"
  "path/filepath"
  "strings"
  "time"

  "github.com/cgtuebingen/infomark-backend/auth/authenticate"
  "github.com/cgtuebingen/infomark-backend/logging"
  "github.com/go-chi/chi"
  "github.com/go-chi/chi/middleware"
  "github.com/go-chi/cors"
  "github.com/go-chi/render"
  "github.com/jmoiron/sqlx"
  _ "github.com/lib/pq"
)

// New configures application resources and routes.
func New(db *sqlx.DB, log bool) (*chi.Mux, error) {
  logger := logging.NewLogger()

  if err := db.Ping(); err != nil {
    logger.WithField("module", "database").Error(err)
    return nil, err
  }

  appAPI, err := NewAPI(db)
  if err != nil {
    logger.WithField("module", "app").Error(err)
    return nil, err
  }

  r := chi.NewRouter()
  r.Use(middleware.Recoverer)
  r.Use(middleware.RequestID)
  r.Use(middleware.Timeout(15 * time.Second))
  if log {
    r.Use(logging.NewStructuredLogger(logger))
  }
  r.Use(render.SetContentType(render.ContentTypeJSON))
  r.Use(corsConfig().Handler)

  // r.Use(authenticate.AuthenticateAccessJWT)
  r.Route("/api", func(r chi.Router) {

    r.Route("/v1", func(r chi.Router) {

      // open routes
      r.Group(func(r chi.Router) {
        r.Post("/auth/token", appAPI.Auth.RefreshAccessTokenHandler)
        r.Post("/auth/sessions", appAPI.Auth.LoginHandler)
        r.Post("/auth/request_password_reset", appAPI.Auth.RequestPasswordResetHandler)
        r.Post("/auth/update_password", appAPI.Auth.UpdatePasswordHandler)
        r.Post("/auth/confirm_email", appAPI.Auth.ConfirmEmailHandler)
        r.Post("/account", appAPI.Account.CreateHandler)
        r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
          w.Write([]byte("pong"))
        })
      })

      // protected routes
      r.Group(func(r chi.Router) {
        r.Use(authenticate.RequiredValidAccessClaims)

        r.Get("/me", appAPI.User.GetMeHandler)
        r.Put("/me", appAPI.User.EditMeHandler)

        r.Route("/users", func(r chi.Router) {
          r.Get("/", appAPI.User.IndexHandler)
          r.Route("/{userID}", func(r chi.Router) {
            r.Use(appAPI.User.Context)
            r.Get("/", appAPI.User.GetHandler)
            r.Put("/", appAPI.User.EditHandler)
            r.Delete("/", appAPI.User.DeleteHandler)
            r.Post("/emails", appAPI.User.SendEmailHandler)
          })
        })

        r.Route("/courses", func(r chi.Router) {
          r.Get("/", appAPI.Course.IndexHandler)
          r.Post("/", appAPI.Course.CreateHandler)
          r.Route("/{courseID}", func(r chi.Router) {
            r.Use(appAPI.Course.Context)
            r.Get("/", appAPI.Course.GetHandler)
            r.Put("/", appAPI.Course.EditHandler)
            r.Delete("/", appAPI.Course.DeleteHandler)

            r.Get("/enrollments", appAPI.Course.IndexEnrollmentsHandler)
            r.Post("/enrollments", appAPI.Course.EnrollHandler)
            r.Delete("/enrollments", appAPI.Course.DisenrollHandler)

            r.Route("/sheets", func(r chi.Router) {
              r.Get("/", appAPI.Sheet.IndexHandler)
              r.Post("/", appAPI.Sheet.CreateHandler)
            })

            r.Post("/emails", appAPI.Course.SendEmailHandler)

            r.Get("/points", appAPI.Course.PointsHandler)
          })
        })

        r.Route("/sheets", func(r chi.Router) {
          r.Route("/{sheetID}", func(r chi.Router) {
            r.Use(appAPI.Sheet.Context)
            r.Get("/", appAPI.Sheet.GetHandler)
            r.Put("/", appAPI.Sheet.EditHandler)
            r.Delete("/", appAPI.Sheet.DeleteHandler)

            r.Route("/tasks", func(r chi.Router) {
              r.Get("/", appAPI.Task.IndexHandler)
              r.Post("/", appAPI.Task.CreateHandler)

            })

            r.Route("/file", func(r chi.Router) {
              r.Get("/", appAPI.Sheet.GetFileHandler)
              r.Post("/", appAPI.Sheet.ChangeFileHandler)

            })
          })
        })

        r.Route("/tasks", func(r chi.Router) {
          r.Route("/{taskID}", func(r chi.Router) {
            r.Use(appAPI.Task.Context)
            r.Get("/", appAPI.Task.GetHandler)
            r.Put("/", appAPI.Task.EditHandler)
            r.Delete("/", appAPI.Task.DeleteHandler)

            r.Route("/public_file", func(r chi.Router) {
              r.Get("/", appAPI.Task.GetPublicTestFileHandler)
              r.Post("/", appAPI.Task.ChangePublicTestFileHandler)
            })

            r.Route("/private_file", func(r chi.Router) {
              r.Get("/", appAPI.Task.GetPrivateTestFileHandler)
              r.Post("/", appAPI.Task.ChangePrivateTestFileHandler)
            })
          })

        })

        r.Get("/account", appAPI.Account.GetHandler)
        r.Get("/account/enrollments", appAPI.Account.GetEnrollmentsHandler)
        r.Get("/account/avatar", appAPI.Account.GetAvatarHandler)
        r.Post("/account/avatar", appAPI.Account.ChangeAvatarHandler)
        r.Delete("/account/avatar", appAPI.Account.DeleteAvatarHandler)
        r.Patch("/account", appAPI.Account.EditHandler)
        r.Delete("/auth/sessions", appAPI.Auth.LogoutHandler)

      })

    })
  })

  workDir, _ := os.Getwd()
  filesDir := filepath.Join(workDir, "static")
  FileServer(r, "/", http.Dir(filesDir))

  return r, nil
}

// FileServer conveniently sets up a http.FileServer handler to serve
// static files from a http.FileSystem.
func FileServer(r chi.Router, path string, root http.FileSystem) {
  if strings.ContainsAny(path, "{}*") {
    panic("FileServer does not permit URL parameters.")
  }

  fs := http.StripPrefix(path, http.FileServer(root))

  if path != "/" && path[len(path)-1] != '/' {
    r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
    path += "/"
  }
  path += "*"

  r.Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    fs.ServeHTTP(w, r)
  }))
}

func corsConfig() *cors.Cors {
  // Basic CORS
  // for more ideas, see: https://developer.github.com/v3/#cross-origin-resource-sharing
  return cors.New(cors.Options{
    AllowedOrigins:   []string{"*"},
    AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
    AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
    ExposedHeaders:   []string{"Link"},
    AllowCredentials: true,
    MaxAge:           86400, // Maximum value not ignored by any of major browsers
  })
}