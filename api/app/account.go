// InfoMark - a platform for managing courses with
//            distributing exercise sheets and testing exercise submissions
// Copyright (C) 2019 ComputerGraphics Tuebingen
//               2020-present InfoMark.org
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
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/render"
	"github.com/infomark-org/infomark/api/helper"
	"github.com/infomark-org/infomark/auth"
	"github.com/infomark-org/infomark/auth/authenticate"
	"github.com/infomark-org/infomark/configuration"
	"github.com/infomark-org/infomark/email"
	"github.com/infomark-org/infomark/model"
	"github.com/infomark-org/infomark/symbol"
	null "gopkg.in/guregu/null.v3"
)

// AccountResource specifies user management handler.
type AccountResource struct {
	Stores *Stores
}

// NewAccountResource create and returns a AccountResource.
func NewAccountResource(stores *Stores) *AccountResource {
	return &AccountResource{
		Stores: stores,
	}
}

// CreateHandler is public endpoint for
// URL: /account
// METHOD: post
// TAG: account
// REQUEST: CreateUserAccountRequest
// RESPONSE: 201,UserResponse
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// RESPONSE: 403,Unauthorized
// SUMMARY:  Create a new user account to register on the site.
// DESCRIPTION:
// The account will be created and a confirmation email will be sent.
// There is no way to set an avatar here and root will be false by default.
func (rs *AccountResource) CreateHandler(w http.ResponseWriter, r *http.Request) {
	// Start from empty Request
	data := &CreateUserAccountRequest{}

	// Parse JSON request into struct
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrBadRequestWithDetails(err))
		return
	}

	// We will ask the user to confirm their email address
	token := null.StringFrom(auth.GenerateToken(32))

	user := &model.User{
		FirstName:         data.User.FirstName,
		LastName:          data.User.LastName,
		Email:             data.User.Email,
		StudentNumber:     data.User.StudentNumber,
		Semester:          data.User.Semester,
		Subject:           data.User.Subject,
		Language:          data.User.Language,
		ConfirmEmailToken: token,
		EncryptedPassword: data.Account.EncryptedPassword,
		Root:              false,
	}

	// create user entry in database
	newUser, err := rs.Stores.User.Create(user)
	if err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}

	render.Status(r, http.StatusCreated)

	// return user information of created entry
	if err := render.Render(w, r, newUserResponse(newUser)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}

	if !configuration.Configuration.Server.Debugging.Enabled {
		err = sendConfirmEmailForUser(configuration.Configuration.Server.Email.From, newUser)
		if err != nil {
			render.Render(w, r, ErrInternalServerErrorWithDetails(err))
			return
		}
	}

}

// sendConfirmEmailForUser will send the confirmation email to activate the account.
func sendConfirmEmailForUser(from string, user *model.User) error {
	// send email
	// Send Email to User
	msg, err := email.NewEmailFromTemplate(from,
		user.Email,
		"Confirm Account Instructions",
		email.ConfirmEmailTemplateEN,
		map[string]string{
			"first_name":            user.FirstName,
			"last_name":             user.LastName,
			"confirm_email_url":     fmt.Sprintf("%s/#/confirmation", configuration.Configuration.Server.ExternalURL()),
			"confirm_email_address": user.Email,
			"confirm_email_token":   user.ConfirmEmailToken.String,
		})

	if err != nil {
		return err
	}
	err = email.DefaultMail.Send(msg)
	if err != nil {
		return err
	}

	return nil
}

// EditHandler is public endpoint for
// URL: /account
// METHOD: patch
// TAG: account
// REQUEST: AccountRequest
// RESPONSE: 204,NoContent
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Updates email or password
// DESCRIPTION:
// This is the only endpoint having PATCH as the backend will automatically only
// update fields which are non-empty. If both are given, it will update both fields.
// If the email should be changed a new confirmation email will be sent and clicking
// on the confirmation link is required to login again.
func (rs *AccountResource) EditHandler(w http.ResponseWriter, r *http.Request) {

	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)

	// make a backup of old data
	user, err := rs.Stores.User.Get(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrNotFound)
		return
	}

	// start from database data
	data := &AccountRequest{}

	// update struct from JSON request
	if err := render.Bind(r, data); err != nil {
		render.Render(w, r, ErrBadRequestWithDetails(err))
		return
	}

	// we require the account-part with at least one value
	if data.OldPlainPassword == "" {
		render.Render(w, r, ErrBadRequestWithDetails(errors.New("old_plain_password in request is missing")))
		return
	}

	// does the submitted old password match with the current active password?
	if !auth.CheckPasswordHash(data.OldPlainPassword, user.EncryptedPassword) {
		render.Render(w, r, ErrBadRequestWithDetails(errors.New("credentials are wrong")))
		return
	}

	// this is the ugly PATCH logic (instead of PUT)
	emailHasChanged := false
	if data.Account.Email != "" {
		emailHasChanged = data.Account.Email != user.Email
	}

	passwordHasChanged := data.Account.PlainPassword != ""

	// make sure email is valid
	if emailHasChanged {
		// we will ask the user to confirm their email address
		user.ConfirmEmailToken = null.StringFrom(auth.GenerateToken(32))
		user.Email = data.Account.Email
	}

	if passwordHasChanged {
		user.EncryptedPassword = data.Account.EncryptedPassword
	}

	if err := rs.Stores.User.Update(user); err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
		return
	}

	// make sure email is valid
	if emailHasChanged {
		err = sendConfirmEmailForUser(configuration.Configuration.Server.Email.From, user)
		if err != nil {
			render.Render(w, r, ErrInternalServerErrorWithDetails(err))
			return
		}
	}

	render.Status(r, http.StatusNoContent)

	if err := render.Render(w, r, newUserResponse(user)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}

}

// GetHandler is public endpoint for
// URL: /account
// METHOD: get
// TAG: account
// RESPONSE: 200,UserResponse
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Retrieve the specific user account from the requesting identity.
// DESCRIPTION:
// It will contain all information as this can only query the own account
func (rs *AccountResource) GetHandler(w http.ResponseWriter, r *http.Request) {
	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)
	user, err := rs.Stores.User.Get(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrNotFound)
		return
	}

	if err := render.Render(w, r, newUserResponse(user)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}

}

// GetAvatarHandler is public endpoint for
// URL: /account/avatar
// METHOD: get
// TAG: account
// RESPONSE: 200,ImageFile
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Retrieve the specific account avatar from the request identity
// DESCRIPTION:
// If there is an avatar for this specific user, this will return the image
// otherwise it will use a default image. We currently support only jpg images.
func (rs *AccountResource) GetAvatarHandler(w http.ResponseWriter, r *http.Request) {

	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)
	file := helper.NewAvatarFileHandle(accessClaims.LoginID)

	if !file.Exists() {
		render.Render(w, r, ErrNotFound)
		return
	}

	if err := file.WriteToBody(w); err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
	}

}

// ChangeAvatarHandler is public endpoint for
// URL: /account/avatar
// METHOD: post
// TAG: account
// REQUEST: Imagefile
// RESPONSE: 204,NoContent
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Change the specific account avatar of the request identity
// DESCRIPTION:
// We currently support only jpg, jpeg,png images.
func (rs *AccountResource) ChangeAvatarHandler(w http.ResponseWriter, r *http.Request) {

	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)

	// get current user
	user, err := rs.Stores.User.Get(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrNotFound)
		return
	}

	if _, err := helper.NewAvatarFileHandle(user.ID).WriteToDisk(r, "file_data"); err != nil {
		render.Render(w, r, ErrBadRequestWithDetails(err))
	}

	user.AvatarURL = null.StringFrom(fmt.Sprintf("/api/v1/users/%s/avatar", strconv.FormatInt(user.ID, 10)))
	if err := rs.Stores.User.Update(user); err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
	}

	render.Status(r, http.StatusOK)
}

// DeleteAvatarHandler is public endpoint for
// URL: /account/avatar
// METHOD: delete
// TAG: account
// RESPONSE: 204,NoContent
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Delete the specific account avatar of the request identity
// DESCRIPTION:
// This is necessary, when a user wants to switch back to a default avatar.
func (rs *AccountResource) DeleteAvatarHandler(w http.ResponseWriter, r *http.Request) {
	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)

	// get current user
	user, err := rs.Stores.User.Get(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrNotFound)
		return
	}

	if err = helper.NewAvatarFileHandle(user.ID).Delete(); err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
	}

	render.Status(r, http.StatusNoContent)
}

// GetEnrollmentsHandler is public endpoint for
// URL: /account/enrollments
// METHOD: get
// TAG: account
// RESPONSE: 200,UserEnrollmentResponseList
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Retrieve the specific account avatar from the request identity
// This lists all course enrollments of the request identity including role.
func (rs *AccountResource) GetEnrollmentsHandler(w http.ResponseWriter, r *http.Request) {
	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)

	// get enrollments
	enrollments, err := rs.Stores.User.GetEnrollments(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
		return
	}

	// render JSON response
	if err = render.RenderList(w, r, rs.newUserEnrollmentsResponse(enrollments)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

// GetExamEnrollmentsHandler is public endpoint for
// URL: /account/exams/enrollments
// METHOD: get
// TAG: account
// RESPONSE: 200,ExamEnrollmentResponseList
// RESPONSE: 400,BadRequest
// RESPONSE: 401,Unauthenticated
// SUMMARY:  Retrieve the specific account avatar from the request identity
// This lists all course enrollments of the request identity including role.
func (rs *AccountResource) GetExamEnrollmentsHandler(w http.ResponseWriter, r *http.Request) {
	accessClaims := r.Context().Value(symbol.CtxKeyAccessClaims).(*authenticate.AccessClaims)

	// get enrollments
	enrollments, err := rs.Stores.Exam.GetEnrollmentsOfUser(accessClaims.LoginID)
	if err != nil {
		render.Render(w, r, ErrInternalServerErrorWithDetails(err))
		return
	}

	// render JSON response
	if err = render.RenderList(w, r, newExamEnrollmentListResponse(enrollments)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}
