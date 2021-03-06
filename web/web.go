package web

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"

	"github.com/doylecnn/new-nsfc-bot/chatbot"
	"github.com/doylecnn/new-nsfc-bot/storage"
	"github.com/doylecnn/new-nsfc-bot/web/middleware"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/sirupsen/logrus"
	"github.com/thinkerou/favicon"
)

// Web is web
type Web struct {
	APPID       string
	Domain      string
	Port        string
	TgBotToken  string
	TgBotClient *tgbotapi.BotAPI
	SecretKey   [32]byte
	Route       *gin.Engine
	AdminID     int
}

// NewWeb return new Web
func NewWeb(token, domain, appID, projectID, port string, adminID int, bot chatbot.ChatBot) (web Web, updates chan tgbotapi.Update) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	secretKey := sha256.Sum256([]byte(token))
	web = Web{
		APPID:       appID,
		Domain:      domain,
		Port:        port,
		TgBotToken:  token,
		TgBotClient: bot.TgBotClient,
		SecretKey:   secretKey,
		Route:       r,
		AdminID:     adminID,
	}
	r.Use(favicon.New("./web/static/favicon.png"))
	r.Static("/ACNH_Turnip_Calculator", "web/static/ACNH_Turnip_Calculator")

	r.LoadHTMLGlob("web/templates/*")

	r.GET("/", web.Index)
	r.GET("/index", web.Index)
	r.GET("/auth", web.Auth)
	r.GET("/login", web.Login)

	authorized := r.Group("/", middleware.TelegramAuth(secretKey))
	{
		authorized.GET("/user/:userid", web.User)
		authorized.GET("/islands", web.Islands)
		authorized.GET("/logout", web.Logout)
	}

	admin := r.Group("/admin", middleware.TelegramAdminAuth(secretKey, web.AdminID))
	{
		admin.GET("/export/:userid", web.export)
		admin.GET("/botrestart", func(c *gin.Context) {
			bot.RestartBot()
		})
	}

	updates = make(chan tgbotapi.Update, bot.TgBotClient.Buffer)
	r.POST("/"+token, func(c *gin.Context) {
		bytes, _ := ioutil.ReadAll(c.Request.Body)

		var update tgbotapi.Update
		json.Unmarshal(bytes, &update)

		updates <- update
	})

	return
}

// Run run the web
func (w Web) Run() {
	w.Route.Run(fmt.Sprintf(":%s", w.Port))
}

type exportData struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	NSAccounts []nsAccount `json:"ns_accounts,omitempty"`
	Games      []game      `json:"games,omitempty"`
	Groups     []group     `json:"groups,omitempty"`
}

type nsAccount struct {
	Name string `json:"name"`
	FC   int64  `json:"friend_code"`
}

type game struct {
	Name string                 `json:"name"`
	Info map[string]interface{} `json:"info"`
}

type group struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

func (w Web) setCookie(c *gin.Context, name, value string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    url.QueryEscape(value),
		MaxAge:   86400,
		Path:     "/",
		Domain:   c.Request.URL.Host,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (w Web) delCookie(c *gin.Context, name string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		Domain:   c.Request.URL.Host,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (w Web) export(c *gin.Context) {
	if v, exists := c.Get("admin_authed"); exists {
		if authed, ok := v.(bool); ok && authed {
			authData, _ := c.Cookie("auth_data_str")
			userID, err := middleware.GetAuthDataInfo(authData, "id")
			if err != nil {
				logrus.WithError(err).Error("auth failed")
				c.Abort()
				return
			}
			userid := c.Param("userid")
			if userID == userid {
				uid, err := strconv.ParseInt(userid, 10, 64)
				if err != nil {
					logrus.WithError(err).Error("auth failed")
					c.Abort()
					return
				}
				if int(uid) == w.AdminID {
					ctx := context.Background()
					us, err := storage.GetAllUsers(ctx)
					if err != nil {
						logrus.WithError(err).Error("auth failed")
						c.Abort()
						return
					}

					var allgroups map[int64]group = make(map[int64]group)
					if gs, err := storage.GetAllGroups(ctx); err == nil {
						for _, g := range gs {
							allgroups[g.ID] = group{ID: g.ID,
								Type:  g.Type,
								Title: g.Title}
						}
					}

					var userinfos []exportData
					for _, u := range us {
						var nsaccounts []nsAccount
						for _, a := range u.NSAccounts {
							nsaccounts = append(nsaccounts, nsAccount{FC: int64(a.FC), Name: a.Name})
						}
						var games []game
						if axi, err := u.GetAnimalCrossingIsland(ctx); err == nil {
							if axi != nil {
								var pricehistory map[int64]map[string]interface{} = map[int64]map[string]interface{}{}
								if ph, err := storage.GetPriceHistory(ctx, int(u.ID)); err == nil {
									for _, p := range ph {
										pricehistory[p.Date.Unix()] = map[string]interface{}{
											"date":      p.Date.Format("2006-01-02 15:04:05 -0700"),
											"price":     int(p.Price),
											"timezone":  p.Timezone.String(),
											"dateInLoc": p.LocationDateTime().Format("2006-01-02 15:04:05 -0700"),
										}
									}
								}
								g := game{Name: "AnimalCrossing",
									Info: map[string]interface{}{
										"airportIsOpen":  axi.AirportIsOpen,
										"islandBaseInfo": axi.BaseInfo,
										"timezone":       axi.Timezone.String(),
										"info":           axi.Info,
										"fruits":         axi.Fruits,
										"hemisphere":     axi.Hemisphere,
										"name":           axi.Name,
										"owner":          axi.Owner,
										"priceHistory":   pricehistory,
									},
								}
								games = append(games, g)
							}
						}
						var groups []group
						for _, gid := range u.GroupIDs {
							groups = append(groups, allgroups[gid])
						}
						ui := exportData{
							ID:         u.ID,
							Name:       u.Name,
							NSAccounts: nsaccounts,
							Games:      games,
							Groups:     groups,
						}
						userinfos = append(userinfos, ui)
					}
					c.SecureJSON(http.StatusOK, userinfos)
					return
				}
				logrus.Error("not admin")
				c.Abort()
				return

			}
			logrus.Error("not admin")
			c.Abort()
			return
		}
	}
	c.Redirect(200, "login.html")
}
