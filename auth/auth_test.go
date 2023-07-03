// SPDX-License-Identifier: ice License 1.0

package auth

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"
	stdlibtime "time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ice-blockchain/wintr/auth/fixture"
	"github.com/ice-blockchain/wintr/auth/internal"
	"github.com/ice-blockchain/wintr/time"
)

const (
	testApplicationYAMLKey = "self"
)

// .
var (
	//nolint:gochecknoglobals // It's a stateless singleton for tests.
	client Client
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*stdlibtime.Second)
	client = New(ctx, testApplicationYAMLKey)
	defer func() {
		if e := recover(); e != nil {
			cancel()
			log.Panic(e)
		}
	}()
	exitCode := m.Run()
	cancel()
	os.Exit(exitCode) //nolint:gocritic // That's intended.
}

func TestVerifyFBToken_ValidToken(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()

	uid, idToken := fixture.CreateUser("app")
	defer fixture.DeleteUser(uid)

	token, err := client.VerifyToken(ctx, idToken)
	require.NoError(t, err)
	require.NotNil(t, token)
	require.NotEmpty(t, token.UserID)
	require.Equal(t, uid, token.UserID)
	require.NotEmpty(t, token.Email)
	require.Equal(t, "app", token.Role)
	require.NotEmpty(t, token.Claims)
}

func TestVerifyFBToken_InvalidToken(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()

	token, err := client.VerifyToken(ctx, "invalid token")
	require.Error(t, err)
	require.Nil(t, token)
}

func TestUpdateCustomClaims_Success(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()

	uid, _ := fixture.CreateUser("app")
	defer fixture.DeleteUser(uid)

	user, err := fixture.GetUser(ctx, uid)
	require.NoError(t, err)
	require.EqualValues(t, map[string]any{"role": "app"}, user.CustomClaims)
	require.NoError(t, client.UpdateCustomClaims(ctx, uid, map[string]any{"a": 1, "b": map[string]any{"c": "x"}}))
	require.NoError(t, client.UpdateCustomClaims(ctx, uid, map[string]any{"b": map[string]any{"d": "y"}}))
	require.ErrorIs(t, client.(*auth).fb.UpdateCustomClaims(ctx, uuid.NewString(), map[string]any{"a": 1}), ErrUserNotFound) //nolint:forcetypeassert // .
	// Ice no-op is called when user does not exist in firebase.
	require.NoError(t, client.UpdateCustomClaims(ctx, uuid.NewString(), map[string]any{"a": 1}))
	user, err = fixture.GetUser(ctx, uid)
	require.NoError(t, err)
	require.EqualValues(t, map[string]any{"a": 1.0, "b": map[string]any{"c": "x", "d": "y"}, "role": "app"}, user.CustomClaims)
}

func TestDeleteUser_Success(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()

	uid, _ := fixture.CreateUser("app")

	user, err := fixture.GetUser(ctx, uid)
	require.NoError(t, err)
	require.NotEmpty(t, user.PhoneNumber)
	require.NoError(t, client.DeleteUser(ctx, uid))
	require.NoError(t, client.DeleteUser(ctx, uuid.NewString()), ErrUserNotFound)
	_, err = fixture.GetUser(ctx, uid)
	require.NotNil(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "no user exists with the"))
}

func TestVerifyIceToken_ValidToken(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()
	var (
		seq    = int64(0)
		userID = "bogus"
		email  = "bogus@bogus.com"
		role   = "author"
	)
	refreshToken, accessToken, err := fixture.GenerateIceTokens(userID, role)
	require.NoError(t, err)

	verifiedAccessToken, err := client.VerifyToken(ctx, accessToken)
	require.NoError(t, err)

	assert.NotEmpty(t, email, verifiedAccessToken.Email)
	assert.Equal(t, role, verifiedAccessToken.Role)
	assert.Equal(t, userID, verifiedAccessToken.UserID)
	assert.NotEmpty(t, email, verifiedAccessToken.Claims["email"])
	assert.Equal(t, seq, verifiedAccessToken.Claims["seq"])
	assert.Equal(t, role, verifiedAccessToken.Claims["role"])

	_, err = client.VerifyToken(ctx, refreshToken)
	require.Error(t, err, ErrWrongTypeToken)
}

func TestVerifyIceToken_InvalidToken(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()
	_, err := client.VerifyToken(ctx, "wrong")
	require.Error(t, err)
}

func TestGenerateIceTokens_Valid(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()
	var (
		now      = time.Now()
		seq      = int64(0)
		hashCode = int64(0)
		userID   = "bogus"
		email    = "bogus@bogus.com"
		deviceID = "00000000-0000-0000-0000-000000000001"
		role     = "author"
		claims   = map[string]any{"role": role, "reallyCustomClaim": "claimData"}
	)
	refreshToken, accessToken, err := client.GenerateTokens(now, userID, deviceID, email, hashCode, seq, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, refreshToken)
	assert.NotEmpty(t, accessToken)

	verifiedAccessToken, err := client.VerifyToken(ctx, accessToken)
	require.NoError(t, err)
	assert.NotEmpty(t, email, verifiedAccessToken.Email)
	assert.Equal(t, role, verifiedAccessToken.Role)
	assert.Equal(t, userID, verifiedAccessToken.UserID)
	assert.NotEmpty(t, email, verifiedAccessToken.Claims["email"])
	assert.Equal(t, seq, verifiedAccessToken.Claims["seq"])
	assert.Equal(t, role, verifiedAccessToken.Claims["role"])
	assert.Equal(t, deviceID, verifiedAccessToken.Claims["deviceUniqueID"])
	assert.Equal(t, "claimData", verifiedAccessToken.Claims["reallyCustomClaim"])
	_, err = client.VerifyToken(ctx, refreshToken)
	require.Error(t, err, ErrWrongTypeToken)
}

func TestUpdateCustomClaims_Ice(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()
	var (
		userID = "ice"
		claims = map[string]any{
			"role": "author",
		}
	)
	require.NoError(t, client.UpdateCustomClaims(ctx, userID, claims))
}

func TestDeleteUser_Ice(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*stdlibtime.Second)
	defer cancel()
	userID := "ice"

	assert.Nil(t, client.DeleteUser(ctx, userID))
}

func TestParseToken_Parse(t *testing.T) { //nolint:funlen // .
	t.Parallel()
	var (
		now      = time.Now()
		seq      = int64(0)
		hashCode = int64(0)
		userID   = "bogus"
		email    = "bogus@bogus.com"
		role     = "author"
		deviceID = "00000000-0000-0000-0000-000000000001"
		claims   = map[string]any{"role": role}
	)
	refreshToken, accessToken, err := client.GenerateTokens(now, userID, deviceID, email, hashCode, seq, claims)
	require.NoError(t, err)
	assert.NotEmpty(t, refreshToken)
	assert.NotEmpty(t, accessToken)

	accessRes, err := client.ParseToken(accessToken)
	require.NoError(t, err)
	issuer, err := accessRes.GetIssuer()
	require.NoError(t, err)
	assert.Equal(t, "ice.io/access", issuer)
	subject, err := accessRes.GetSubject()
	require.NoError(t, err)
	assert.Equal(t, userID, subject)
	require.NoError(t, err)
	assert.Equal(t, role, accessRes.Role)
	assert.Equal(t, email, accessRes.Email)
	assert.Equal(t, deviceID, accessRes.DeviceUniqueID)
	assert.Equal(t, hashCode, accessRes.HashCode)
	assert.Equal(t, seq, accessRes.Seq)

	refreshRes, err := client.ParseToken(refreshToken)
	require.NoError(t, err)
	accessRes, err = client.ParseToken(accessToken)
	require.NoError(t, err)
	issuer, err = refreshRes.GetIssuer()
	require.NoError(t, err)
	assert.Equal(t, "ice.io/refresh", issuer)
	subject, err = refreshRes.GetSubject()
	require.NoError(t, err)
	assert.Equal(t, userID, subject)
	require.NoError(t, err)
	assert.Equal(t, role, accessRes.Role)
	assert.Equal(t, email, accessRes.Email)
	assert.Equal(t, deviceID, accessRes.DeviceUniqueID)
	assert.Equal(t, hashCode, accessRes.HashCode)
	assert.Equal(t, seq, accessRes.Seq)
}

func TestMetadata_Empty(t *testing.T) {
	t.Parallel()
	var (
		now    = time.Now()
		userID = uuid.NewString()
	)
	metadataToken, err := client.GenerateMetadata(now, userID, map[string]any{})
	require.NoError(t, err)
	assert.NotEmpty(t, metadataToken)

	tok := &Token{UserID: userID}
	err = client.ModifyTokenWithMetadata(tok, metadataToken)
	require.NoError(t, err)
	assert.Equal(t, tok.UserID, userID)
	var decodedMetadata jwt.MapClaims
	err = client.(*auth).ice.VerifyTokenFields(metadataToken, &decodedMetadata) //nolint:forcetypeassert // .
	require.NoError(t, err)
	assert.Equal(t, decodedMetadata["sub"], userID)
	assert.Equal(t, decodedMetadata["iss"], internal.MetadataIssuer)
	assert.Equal(t, int64(decodedMetadata["iat"].(float64)), now.Unix()) //nolint:forcetypeassert // .

	err = client.ModifyTokenWithMetadata(tok, "")
	require.NoError(t, err)
	assert.Equal(t, tok.UserID, userID)
}

func TestMetadata_RegisteredBy(t *testing.T) { //nolint:funlen // .
	t.Parallel()
	testfunc := func(t *testing.T, provider, iceID, firebaseID, result string) {
		t.Helper()
		var (
			now    = time.Now()
			userID = uuid.NewString()
		)
		metadataToken, err := client.GenerateMetadata(now, userID, map[string]any{
			IceIDClaim:                  iceID,
			RegisteredWithProviderClaim: provider,
			FirebaseIDClaim:             firebaseID,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, metadataToken)

		tok := &Token{UserID: userID}
		err = client.ModifyTokenWithMetadata(tok, metadataToken)
		require.NoError(t, err)
		assert.Equal(t, tok.UserID, result)
		var decodedMetadata jwt.MapClaims
		err = client.(*auth).ice.VerifyTokenFields(metadataToken, &decodedMetadata) //nolint:forcetypeassert // .
		require.NoError(t, err)
		assert.Equal(t, decodedMetadata["sub"], userID)
		assert.Equal(t, decodedMetadata["iss"], internal.MetadataIssuer)
		assert.Equal(t, int64(decodedMetadata["iat"].(float64)), now.Unix()) //nolint:forcetypeassert // .
		assert.Equal(t, decodedMetadata[IceIDClaim], iceID)
		assert.Equal(t, decodedMetadata[FirebaseIDClaim], firebaseID)
		assert.Equal(t, decodedMetadata[RegisteredWithProviderClaim], provider)
	}
	t.Run("firebase", func(tt *testing.T) {
		tt.Parallel()
		fbID := uuid.NewString()
		iceID := uuid.NewString()
		testfunc(tt, ProviderFirebase, iceID, fbID, fbID)
	})
	t.Run("ice", func(tt *testing.T) {
		tt.Parallel()
		fbID := uuid.NewString()
		iceID := uuid.NewString()
		testfunc(tt, ProviderIce, iceID, fbID, iceID)
	})
}

func TestMetadataMetadataNotOwnedByToken(t *testing.T) {
	t.Parallel()
	var (
		now    = time.Now()
		userID = uuid.NewString()
	)

	metadataToken, err := client.GenerateMetadata(now, userID, map[string]any{
		IceIDClaim:                  uuid.NewString(),
		RegisteredWithProviderClaim: ProviderFirebase,
		FirebaseIDClaim:             uuid.NewString(),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, metadataToken)

	tok := &Token{UserID: uuid.NewString()} // Metadata was issued for token "userID", not random one.
	err = client.ModifyTokenWithMetadata(tok, metadataToken)
	require.Error(t, err, ErrInvalidToken)
}
