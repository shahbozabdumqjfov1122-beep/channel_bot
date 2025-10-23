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

	// Faqat shu kanal/supergroup uchun
	allowedChannelID := int64(-1003056945596) // Kanal ID sini shu yerga yozing

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	var lastUserID int64    // Oxirgi yozgan foydalanuvchi ID
	var lastMessageID int   // Oxirgi xabar ID
	var lastUserName string // Oxirgi foydalanuvchi ismi

	// 3 daqiqada bir marta avtomatik reply yuboruvchi gorutina
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			if lastUserID != 0 && lastMessageID != 0 {
				reply := tgbotapi.NewMessage(allowedChannelID, "20 daqiqada yozib turamiz ⏰")
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

		// Faqat ruxsat berilgan kanal/supergroup
		if update.Message.Chat.ID != allowedChannelID {
			continue
		}

		// ✅ 1. Yangi foydalanuvchi kirsa — darhol xush kelibsiz desin
		if len(update.Message.NewChatMembers) > 0 {
			for _, newUser := range update.Message.NewChatMembers {
				if newUser.IsBot {
					continue
				}

				msg := tgbotapi.NewMessage(update.Message.Chat.ID,
					"Salom "+newUser.FirstName+"! Kanalga xush kelibsiz 🎉\n!")
				msg.ReplyToMessageID = update.Message.MessageID
				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Xush kelibsiz xabar yuborishda xato:", err)
				} else {
					log.Printf("Yangi foydalanuvchiga xush kelibsiz yuborildi: %s", newUser.FirstName)
				}
			}
			continue
		}

		// ✅ 2. Oddiy xabar — oxirgi foydalanuvchini eslab qolish
		if !update.Message.From.IsBot {
			lastUserID = update.Message.From.ID
			lastMessageID = update.Message.MessageID
			lastUserName = update.Message.From.FirstName
			log.Printf("Oxirgi yozgan foydalanuvchi: %s (%d)", lastUserName, lastUserID)
		}
	}
}
