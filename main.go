package main

import (
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	bot, err := tgbotapi.NewBotAPI("8428800131:AAH0o_pCO7UwRwSfo7OnGJ0q0mXKmA_pQU4")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("Bot authorized on account %s", bot.Self.UserName)

	// Faqat shu kanal/supergroup uchun
	allowedChannelID := int64(-1003056945596)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	var newUsers []tgbotapi.User // Guruhga yangi qo‘shilgan foydalanuvchilar

	// 3 daqiqada bir avtomatik javob yuboruvchi gorutina
	go func() {
		for {
			time.Sleep(3 * time.Minute)
			for _, newUser := range newUsers {
				reply := tgbotapi.NewMessage(allowedChannelID, "20 daqiqada yozib turamiz ⏰")
				_, err := bot.Send(reply)
				if err != nil {
					log.Println("3 daqiqalik xabar yuborishda xato:", err)
				} else {
					log.Printf("Avtomatik javob yuborildi: foydalanuvchi %s", newUser.FirstName)
				}
			}
			// Keyingi davr uchun ro'yxatni tozalaymiz
			newUsers = nil
		}
	}()

	// Komandalar va javoblar
	var commands = map[string]string{
		"salom":      "alik",
		"qalay":      "yaxshi, rahmat!",
		"nima":       "nma nma",
		"vaqt":       "Hozirgi vaqt: " + time.Now().Format("15:04"),
		"yordam":     "Menga yozing,@TM_ESPORTS yordam beraman!",
		"salomlar":   "Salom, do'stim!",
		"rahmat":     "Doimo mamnunman!",
		"kitob":      "Qaysi kitobni o'qiyapsiz?",
		"dasturlash": "Zo'r! Qaysi tilni ishlatyapsiz?",
		"telegram":   "Botlar hayotingizni osonlashtiradi!",
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Faqat ruxsat berilgan kanal/supergroup
		if update.Message.Chat.ID != allowedChannelID {
			continue
		}

		// ✅ 1. Yangi foydalanuvchi kirsa — xush kelibsiz
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

				// Yangi foydalanuvchini avtomatik javob ro'yxatiga qo‘shamiz
				newUsers = append(newUsers, newUser)
			}
			continue
		}

		// ✅ 2. Oddiy xabar — buyruq tekshirish
		if !update.Message.From.IsBot {
			text := update.Message.Text
			if response, ok := commands[text]; ok {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, response)
				msg.ReplyToMessageID = update.Message.MessageID
				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Buyruqqa javob berishda xato:", err)
				} else {
					log.Printf("Foydalanuvchi %s: %s → Javob: %s",
						update.Message.From.FirstName, text, response)
				}
			}
		}
	}
}
