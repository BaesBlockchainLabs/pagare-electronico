package models

type BCFAssetData struct {
	Type        string `json:"type"`
	App         string `json:"app,omitempty"`
	From        string `json:"from,omitempty"`
	Token       bool   `json:"token"`
	Namespace   string `json:"namespace,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Amount      int    `json:"amount,omitempty"`
}

type BCFAsset struct {
	ID   string       `json:"id"`
	Data BCFAssetData `json:"data"`
}

type BCFAssetResponse struct {
	Ok    bool      `json:"ok"`
	Msg   string    `json:"msg"`
	ID    string    `json:"id,omitempty"`
	Cost  float64   `json:"cost,omitempty"`
	Asset *BCFAsset `json:"asset,omitempty"`
}

type BCFAssetListResponse struct {
	Ok     bool       `json:"ok"`
	Msg    string     `json:"msg"`
	Count  BCFCount   `json:"count,omitempty"`
	Assets []BCFAsset `json:"assets,omitempty"`
}

type BCFCount struct {
	Total int      `json:"total"`
	Pages BCFPages `json:"pages"`
}

type BCFPages struct {
	Current int `json:"current"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

type BCFHistoryEntry struct {
	ID       string                 `json:"id"`
	Metadata map[string]interface{} `json:"metadata"`
}

type BCFHistoryResponse struct {
	Ok      bool              `json:"ok"`
	Msg     string            `json:"msg"`
	History []BCFHistoryEntry `json:"history"`
}

type BCFOwner struct {
	Pub    string `json:"pub"`
	Amount int    `json:"amount"`
}

type BCFOwnersResponse struct {
	Ok     bool       `json:"ok"`
	Msg    string     `json:"msg"`
	Owners []BCFOwner `json:"owners,omitempty"`
	Amount int        `json:"amount,omitempty"`
}

type BCFKeypairResponse struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
	Pub string `json:"pub"`
	Pvt string `json:"pvt"`
}

type BCFDIDResponse struct {
	Ok       bool          `json:"ok"`
	Msg      string        `json:"msg"`
	Identity *IdentidadDID `json:"identity,omitempty"`
}

type BCFCryptoResponse struct {
	Ok      bool   `json:"ok"`
	Msg     string `json:"msg"`
	Message string `json:"message,omitempty"`
	Nonce   string `json:"nonce,omitempty"`
}

type BCFSimpleResponse struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
}

type BCFTimeResponse struct {
	Ok        bool   `json:"ok"`
	Msg       string `json:"msg"`
	Timestamp int64  `json:"timestamp"`
}

type BCFHelloResponse struct {
	Ok  bool   `json:"ok"`
	Msg string `json:"msg"`
}
