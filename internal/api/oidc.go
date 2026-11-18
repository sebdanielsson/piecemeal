package api

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/sebdanielsson/piecemeal/internal/auth"
)

// registerOIDC adds the browser redirects of OIDC sign-in. They are plain
// handlers rather than API operations: browsers navigate to them.
func (a *API) registerOIDC(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Prefix+"/auth/oidc/login", a.oidcLogin)
	mux.HandleFunc("GET "+Prefix+"/auth/oidc/callback", a.oidcCallback)
}

const oidcCookiePath = Prefix + "/auth/oidc"

func (a *API) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		http.Error(w, auth.ErrOIDCDisabled.Error(), http.StatusNotFound)
		return
	}
	redirect, state, err := a.OIDC.Begin(r.Context(), r.URL.Query().Get("next"))
	if err != nil {
		a.Log.Error("starting OIDC sign-in", "error", err)
		signInError(w, r, "The sign-in provider is unavailable. Try again, or sign in with a password.")
		return
	}
	c := a.Auth.Cookie(auth.StateCookieName(), state, 600) //nolint:gosec // Secure is configurable for plain-HTTP setups; HttpOnly and SameSite=Lax are set
	c.Path = oidcCookiePath
	http.SetCookie(w, c)
	// The provider's authorization endpoint, from its discovery document.
	http.Redirect(w, r, redirect, http.StatusFound) //nolint:gosec // not user-controlled
}

func (a *API) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if a.OIDC == nil {
		http.Error(w, auth.ErrOIDCDisabled.Error(), http.StatusNotFound)
		return
	}
	gone := a.Auth.Cookie(auth.StateCookieName(), "", -1) //nolint:gosec // Secure is configurable for plain-HTTP setups; HttpOnly and SameSite=Lax are set
	gone.Path = oidcCookiePath
	http.SetCookie(w, gone)

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		signInError(w, r, "The sign-in provider refused: "+e)
		return
	}
	var cookieState string
	if c, err := r.Cookie(auth.StateCookieName()); err == nil {
		cookieState = c.Value
	}
	user, next, err := a.OIDC.Finish(r.Context(), cookieState, q.Get("state"), q.Get("code"))
	switch {
	case errors.Is(err, auth.ErrOIDCState), errors.Is(err, auth.ErrNotAllowed):
		signInError(w, r, err.Error())
		return
	case err != nil:
		a.Log.Error("finishing OIDC sign-in", "error", err)
		signInError(w, r, "Sign-in failed. Try again, or sign in with a password.")
		return
	}
	token, _, err := a.Auth.OpenSession(r.Context(), r, user.ID, auth.KindWeb)
	if err != nil {
		a.Log.Error("opening a session after OIDC sign-in", "error", err)
		signInError(w, r, "Sign-in failed.")
		return
	}
	http.SetCookie(w, a.Auth.Cookie(auth.CookieWeb, token, a.Auth.SessionMaxAge()))
	// OIDC.Begin keeps only same-site paths in next.
	http.Redirect(w, r, next, http.StatusFound) //nolint:gosec // a same-site path, checked in OIDC.Begin
}

// signInError sends the browser back to the sign-in page with a message.
func signInError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusFound)
}
