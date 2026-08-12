package wechat

type BaseInfo struct {
	ChannelVersion string `json:"channel_version,omitempty"`
	BotAgent       string `json:"bot_agent,omitempty"`
}

type TextItem struct {
	Text string `json:"text,omitempty"`
}

type VoiceItem struct {
	Text     string    `json:"text,omitempty"`
	Media    *CDNMedia `json:"media,omitempty"`
	PlayTime int       `json:"playtime,omitempty"`
}

// CDNMedia contains opaque iLink media references. Download, decryption, and
// codec conversion stay inside the WeChat transport.
type CDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
}

type ImageItem struct {
	Media   *CDNMedia `json:"media,omitempty"`
	MidSize int64     `json:"mid_size,omitempty"`
}

type VideoItem struct {
	Media     *CDNMedia `json:"media,omitempty"`
	VideoSize int64     `json:"video_size,omitempty"`
}

type FileItem struct {
	Media    *CDNMedia `json:"media,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	Len      string    `json:"len,omitempty"`
}

type MessageItem struct {
	Type      int        `json:"type,omitempty"`
	TextItem  *TextItem  `json:"text_item,omitempty"`
	VoiceItem *VoiceItem `json:"voice_item,omitempty"`
	ImageItem *ImageItem `json:"image_item,omitempty"`
	VideoItem *VideoItem `json:"video_item,omitempty"`
	FileItem  *FileItem  `json:"file_item,omitempty"`
}

type Message struct {
	MessageID    int64         `json:"message_id,omitempty"`
	ClientID     string        `json:"client_id,omitempty"`
	FromUserID   string        `json:"from_user_id,omitempty"`
	ToUserID     string        `json:"to_user_id,omitempty"`
	SessionID    string        `json:"session_id,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type,omitempty"`
	MessageState int           `json:"message_state,omitempty"`
	ItemList     []MessageItem `json:"item_list,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
}

type APIResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

type GetUploadURLResponse struct {
	APIResponse
	UploadParam   string `json:"upload_param,omitempty"`
	UploadFullURL string `json:"upload_full_url,omitempty"`
}

type GetConfigResponse struct {
	APIResponse
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type TypingStatus int

const (
	Typing        TypingStatus = 1
	TypingStopped TypingStatus = 2
)

type GetUpdatesResponse struct {
	Ret                  int       `json:"ret,omitempty"`
	ErrCode              int       `json:"errcode,omitempty"`
	ErrMsg               string    `json:"errmsg,omitempty"`
	Messages             []Message `json:"msgs,omitempty"`
	GetUpdatesBuf        string    `json:"get_updates_buf,omitempty"`
	LongPollingTimeoutMS int       `json:"longpolling_timeout_ms,omitempty"`
}

type QRCodeResponse struct {
	QRCode         string `json:"qrcode"`
	QRCodeImageURL string `json:"qrcode_img_content"`
}

type QRStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token,omitempty"`
	BotID        string `json:"ilink_bot_id,omitempty"`
	BaseURL      string `json:"baseurl,omitempty"`
	UserID       string `json:"ilink_user_id,omitempty"`
	RedirectHost string `json:"redirect_host,omitempty"`
}
