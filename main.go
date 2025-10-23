package main

import (
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI("8428800131:AAERPZZzDGGcgoeUxOpFxptQ80hB1_W_mSk")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Bot authorized on account %s", bot.Self.UserName)

	allowedChannelID := int64(-1003056945596) // Kanal ID sini shu yerga yozing

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	type UserInfo struct {
		ChatID         int64
		ReplyMessageID int
		FirstName      string
	}

	// Slice barcha foydalanuvchilarni saqlash uchun
	var allUsers []UserInfo

	// Har 70 daqiqada barcha foydalanuvchilarga xabar yuborish
	go func() {
		ticker := time.NewTicker(70 * time.Minute)
		for range ticker.C {
			for _, user := range allUsers {
				msg := tgbotapi.NewMessage(user.ChatID,
					"Salom "+user.FirstName+"!tiriklar bormi!")
				msg.ReplyToMessageID = user.ReplyMessageID
				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Takroriy xabar yuborishda xato:", err)
				}
			}
		}
	}()

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if update.Message.Chat.ID != allowedChannelID {
			continue // faqat ruxsat berilgan kanal/supergroup
		}

		if len(update.Message.NewChatMembers) > 0 {
			for _, newUser := range update.Message.NewChatMembers {
				if newUser.IsBot {
					continue
				}

				// Reply xabari
				reply := tgbotapi.NewMessage(update.Message.Chat.ID,
					"Salom "+newUser.FirstName+"! Kanalga xush kelibsiz 🎉\nHa kazolar!")
				reply.ReplyToMessageID = update.Message.MessageID

				sentMsg, err := bot.Send(reply)
				if err != nil {
					log.Println("Xabar yuborishda xato:", err)
					continue
				}

				// Foydalanuvchini umumiy listga qo‘shish
				allUsers = append(allUsers, UserInfo{
					ChatID:         sentMsg.Chat.ID,
					ReplyMessageID: sentMsg.MessageID,
					FirstName:      newUser.FirstName,
				})
			}
		}
	}
}
