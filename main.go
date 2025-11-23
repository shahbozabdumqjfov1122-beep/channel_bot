package main

import (
	"fmt"
	"html"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI("8497820416:AAHbGFJU5NP4rrluFgOxBrwsFUCbMXhRuHk")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Bot authorized on account %s", bot.Self.UserName)

	// Faqat shu kanal uchun
	allowedChannelID := int64(-1003316396409)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	var lastUserID int64
	var lastMessageID int
	var lastUserName string

	// 3 daqiqada avtomatik javob yuboruvchi gorutina
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			if lastUserID != 0 && lastMessageID != 0 {
				reply := tgbotapi.NewMessage(allowedChannelID, "yozib turamiz ⏰")
				reply.ReplyToMessageID = lastMessageID
				_, err := bot.Send(reply)
				if err != nil {
					log.Println("3 daqiqalik xabar yuborishda xato:", err)
				} else {
					log.Printf("Avtomatik javob yuborildi: foydalanuvchi %s", lastUserName)
				}
			}
		}
	}()

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Faqat shu kanal
		if update.Message.Chat.ID != allowedChannelID {
			continue
		}

		// Yangi foydalanuvchi kirsa — xush kelibsiz
		if len(update.Message.NewChatMembers) > 0 {
			for _, newUser := range update.Message.NewChatMembers {
				if newUser.IsBot {
					continue
				}

				var msg tgbotapi.MessageConfig

				if newUser.UserName != "" {
					// Username bo'lsa — plain text @username (bu avtomatik kliklanadi)
					text := "Salom @" + newUser.UserName + "! Kanalga xush kelibsiz 🎉"
					msg = tgbotapi.NewMessage(update.Message.Chat.ID, text)
					// Hech qanday ParseMode qo'ymaymiz — shunda "_" va boshqa belgilar parse xatosiga sabab bo'lmaydi
				} else {
					// Username yo'q bo'lsa — HTML mention orqali clickable qilib qo'yamiz
					// FirstName ni html.EscapeString bilan himoyalaymiz
					escapedName := html.EscapeString(newUser.FirstName)
					htmlText := fmt.Sprintf("Salom <a href=\"tg://user?id=%d\">%s</a>! Kanalga xush kelibsiz 🎉", newUser.ID, escapedName)
					msg = tgbotapi.NewMessage(update.Message.Chat.ID, htmlText)
					msg.ParseMode = "HTML"
				}

				msg.ReplyToMessageID = update.Message.MessageID

				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Xush kelibsiz xabar yuborishda xato:", err)
				} else {
					log.Printf("Yangi foydalanuvchi uchun xush kelibsiz yuborildi: %v", newUser.UserName)
				}
			}
			continue
		}

		// Oddiy xabar — oxirgi foydalanuvchini eslab qolish
		if !update.Message.From.IsBot {
			lastUserID = update.Message.From.ID
			lastMessageID = update.Message.MessageID
			lastUserName = update.Message.From.FirstName
			log.Printf("Oxirgi yozgan foydalanuvchi: %s (%d)", lastUserName, lastUserID)
		}
	}
}
