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

	// 2 ta kanal ID
	allowedChannelIDs := []int64{
		-1003316396409, // 1-kanal
		-1002338872199, // 2-kanal
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	var lastUserID int64
	var lastMessageID int
	var lastUserName string

	// ---------------------------
	// 🔥 3 daqiqada avtomatik reply
	// ---------------------------
	go func() {
		for {
			time.Sleep(3 * time.Minute)

			if lastUserID != 0 && lastMessageID != 0 {
				for _, chatID := range allowedChannelIDs {
					reply := tgbotapi.NewMessage(chatID, "yozib turamiz ⏰")
					reply.ReplyToMessageID = lastMessageID
					bot.Send(reply)
				}
			}
		}
	}()

	// ---------------------------
	// 🔥 Asosiy bot sikli
	// ---------------------------
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Faqat ruxsat berilgan kanallar
		allowed := false
		for _, id := range allowedChannelIDs {
			if update.Message.Chat.ID == id {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}

		// ----------------------------------------
		// 🔥 1. Yangi foydalanuvchi kirsa — WELCOME
		// ----------------------------------------
		if len(update.Message.NewChatMembers) > 0 {
			for _, newUser := range update.Message.NewChatMembers {

				if newUser.IsBot {
					continue
				}

				var msg tgbotapi.MessageConfig

				if newUser.UserName != "" {
					// @username bilan, parse mode yo'q
					text := "Salom @" + newUser.UserName + "! Kanalga xush kelibsiz 🎉"
					msg = tgbotapi.NewMessage(update.Message.Chat.ID, text)
				} else {
					// Username bo‘lmasa — HTML mention
					escaped := html.EscapeString(newUser.FirstName)
					htmlText := fmt.Sprintf(
						"Salom <a href=\"tg://user?id=%d\">%s</a>! Kanalga xush kelibsiz 🎉",
						newUser.ID, escaped)
					msg = tgbotapi.NewMessage(update.Message.Chat.ID, htmlText)
					msg.ParseMode = "HTML"
				}

				msg.ReplyToMessageID = update.Message.MessageID

				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Xush kelibsiz xabar yuborishda xato:", err)
				} else {
					log.Printf("Welcome yuborildi: %s", newUser.FirstName)
				}

				// oxirgi userni eslab qolish
				lastUserID = newUser.ID
				lastMessageID = update.Message.MessageID
				lastUserName = newUser.FirstName
			}
			continue
		}

		// ----------------------------------------
		// 🔥 2. Oddiy xabar yozgan userni eslab qolish
		// ----------------------------------------
		if !update.Message.From.IsBot {
			lastUserID = update.Message.From.ID
			lastMessageID = update.Message.MessageID
			lastUserName = update.Message.From.FirstName
			log.Printf("Oxirgi yozgan: %s (%d)", lastUserName, lastUserID)
		}
	}
}
