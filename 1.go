package main

import (
	"context"
	"encoding/json"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var BOT_TOKEN = "8078404751:AAENxW4oFQzkih4e1zq8ZrAZ9DUabZZuV5Y"

const MAIN_ADMIN_ID int64 = 7518992824

var admins = map[int64]bool{
	MAIN_ADMIN_ID: true, // Asosiy admin
}

// ====== Fayl nomlari  ====== \\

const (
	ANIME_STORAGE_FILE = "anime_data.json"
	ANIME_INFO_FILE    = "anime_info.json"
	ADMIN_CONFIG_FILE  = "admin_config.json"
	ANIME_PHOTOS_FILE  = "anime_photos.json" // Qavsni bu yerdan olib tashladik
) // Qavs bu yerda bo'lishi kerak

// ====== Tiplar (Structs) ====== \\
type ContentItem struct {
	Kind   string
	FileID string
	Text   string
}

type Channel struct {
	ChatID   int64
	Username string
}

type UploadTask struct {
	UserID int64
	Code   string
	Item   ContentItem
}

// Foydalanuvchi sahifasi
type UserPage struct {
	Name  string
	Items []ContentItem
	Page  int
}

// Admin konfiguratsiyasi
type AdminConfig struct {
	Admins   map[int64]bool
	Channels map[int64]string
	AllUsers map[int64]time.Time // user qo'shilgan vaqt bilan
}
type VIPUser struct {
	UserID   int64
	ExpireAt time.Time
}

const vipPerPage = 5

// ====== Foydalanuvchilar bilan ishlash

var (
	usersMutex      sync.RWMutex
	userPages       = make(map[int64]*UserPage)      // userID → UserPage
	userLastActive  = make(map[int64]time.Time)      // userID → oxirgi faoliyat vaqti
	userJoinedAt    = make(map[int64]time.Time)      // userID → botga qo'shilgan vaqt
	users           = map[int64]bool{}               // bot foydalanuvchilari
	startUsers      = make(map[int64]string)         // userID → boshlang‘ich xabar yoki state
	blockedUsers    = make(map[int64]bool)           // userID → bloklangan bo‘lsa true
	pendingRequests = make(map[int64]map[int64]bool) // [UserID][ChannelID]bool
	allUsers        = make(map[int64]bool)           // bot foydalanuvchilari
	startCount      int
	searchStats     = make(map[string]int)
	statsMutex      sync.Mutex
	requestMutex    sync.RWMutex
	userJoined      = make(map[int64]time.Time) // foydalanuvchilar qo‘shilgan vaqt
	userActive      = make(map[int64]time.Time) // foydalanuvchilar oxirgi faoliyat
)

// Rejalashtirilgan post strukturasi
type ScheduledPost struct {
	ID       int
	AdminID  int64
	Content  ContentItem
	SendTime time.Time
	ChatIDs  []int64
}

var (
	scheduledPosts = make(map[int]*ScheduledPost)
	scheduleAutoID = 1
	scheduleMutex  sync.Mutex
)

// ====== Anime bilan ishlash
var (
	storageMutex = sync.RWMutex{}
	infoMutex    = sync.RWMutex{}

	animeStorage = make(map[string][]ContentItem) // animeCode yoki animeName → []ContentItem
	animeInfo    = make(map[string]string)        // animeCode yoki animeName → ma'lumot
)
var userMenu = tgbotapi.NewReplyKeyboard(
	tgbotapi.NewKeyboardButtonRow(
		tgbotapi.NewKeyboardButton("🔍 Rasm/Video orqali qidirish"),
	),
)

type AnimeItem struct {
	Link string `json:"link"`
	Kind string `json:"kind"` // Bu yerda 'Kind' (video yoki subtitr turi) saqlanadi
}

// ====== Adminlar bilan ishlash
var (
	adminIDs          = map[int64]bool{MAIN_ADMIN_ID: true}
	adminState        = make(map[int64]string)    // adminID → hozirgi holat
	adminTempID       = make(map[int64]int64)     // adminID → userID yoki boshqa ID
	animeNameTemp     = make(map[int64]string)    // adminID → animeName
	animeCodeTemp     = make(map[int64]string)    // adminID → animeCode
	adminTempChannels = make(map[int64][]Channel) // adminID → []Channel
	animePhotos       = make(map[string]string)
	adminMutex        sync.Mutex
)
var userState = make(map[int64]string)
var (
	broadcastCancel context.CancelFunc
	isBroadcasting  bool
	broadcastMutex  sync.Mutex
)

// ====== Upload & Broadcast
var (
	uploadQueue    = make(chan UploadTask, 1000)
	broadcastCache = make(map[int64]*tgbotapi.Message)
)

// ====== VIP foydalanuvchilar
var (
	vipUsers map[int64]VIPUser
	vipMutex sync.RWMutex
)
var animePhotoMap = make(map[string]string) // code -> fileID
// ====== Kanallar
var channels = make(map[int64]string) // channelID → channelUsername yoki info

// Config strukturasi JSON dagi Admins, Channels va AllUsers ni o'qiydi
type Config struct {
	Admins   map[string]bool   `json:"Admins"`
	Channels map[string]string `json:"Channels"`
	AllUsers map[string]string `json:"AllUsers"`
}

func main() {
	// ⚡ Bot ma'lumotlarini yuklash
	loadData()
	loadRequests()
	loadAnimePhotos()
	bot, err := tgbotapi.NewBotAPI(BOT_TOKEN)
	if err != nil {
		log.Fatal(err)
	}
	if animeInfo == nil {
		animeInfo = make(map[string]string)
	}
	if animeStorage == nil {
		animeStorage = make(map[string][]ContentItem)
	}
	if animePhotos == nil {
		animePhotos = make(map[string]string)
	}
	if adminState == nil {
		adminState = make(map[int64]string)
	}
	if animeCodeTemp == nil {
		animeCodeTemp = make(map[int64]string)
	}
	if animeNameTemp == nil {
		animeNameTemp = make(map[int64]string)
	}
	initQueue(bot)
	startVIPChecker()

	log.Println("Bot ishga tushdi...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "callback_query", "chat_join_request", "chat_member"}

	updates := bot.GetUpdatesChan(u)

	// FAQAT BITTA FOR TSIKLI BO'LISHI SHART!
	for update := range updates {
		// 1️⃣ Kanalga so'rov yuborilsa (Join Request)
		if update.ChatJoinRequest != nil {
			uID := update.ChatJoinRequest.From.ID
			cID := update.ChatJoinRequest.Chat.ID // Qaysi kanalga so'rov keldi

			requestMutex.Lock()
			if pendingRequests == nil {
				pendingRequests = make(map[int64]map[int64]bool)
			}
			if pendingRequests[uID] == nil {
				pendingRequests[uID] = make(map[int64]bool)
			}
			pendingRequests[uID][cID] = true // Aynan shu kanalga so'rov yuborilganini belgilaymiz
			requestMutex.Unlock()

			saveRequests() // Ma'lumotni saqlash
			log.Printf("📥 [JOIN REQUEST] Foydalanuvchi %d, %d idli kanalga so'rov yubordi!", uID, cID)
			continue
		}

		// Foydalanuvchi ID sini aniqlash (Message yoki CallbackQuery dan)
		var currentUserID int64
		if update.Message != nil {
			currentUserID = update.Message.From.ID
		} else if update.CallbackQuery != nil {
			currentUserID = update.CallbackQuery.From.ID
		} else {
			continue // Boshqa turdagi update bo'lsa o'tkazib yuboramiz
		}

		// 2️⃣ "Tekshirish" tugmasi (CallbackQuery) bosilganda
		if update.CallbackQuery != nil && update.CallbackQuery.Data == "check_sub" {
			isOk, missing := checkMembership(bot, currentUserID, true)
			if isOk {
				bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ Tasdiqlandi!"))
				bot.Send(tgbotapi.NewDeleteMessage(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID))
				bot.Send(tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "✅ Rahmat! Endi botdan foydalanishingiz mumkin. /start bosing."))
			} else {
				bot.Request(tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "❌ Siz hali barcha kanallarga so'rov yubormagansiz!"))
				handleMembershipCheck(bot, update.CallbackQuery.Message.Chat.ID, missing)
			}
			continue
		}

		// 3️⃣ ASOSIY FILTR: Har qanday xabar kelganda birinchi bo'lib tekshiramiz
		isOk, missing := checkMembership(bot, currentUserID, false)
		if !isOk {
			// Agar admin bo'lsa, tekshiruvsiz o'tkazish mumkin (ixtiyoriy)
			handleMembershipCheck(bot, currentUserID, missing)
			continue // Botning qolgan qismi ishlamaydi!
		}

		// 4️⃣ Agar hamma narsa OK bo'lsa, botning qolgan mantiqi ishlaydi
		go handleUpdate(bot, update)
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	var userID int64
	var chatID int64
	var text string
	if update.Message != nil {
		userID = update.Message.From.ID
		chatID = update.Message.Chat.ID
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		chatID = update.CallbackQuery.Message.Chat.ID
	} else {
		return
	}

	adminMutex.Lock()
	state := adminState[userID]
	adminMutex.Unlock()

	// --- YANGI QISM: Rasm qabul qilish ---
	if update.Message != nil && update.Message.Photo != nil {
		// Holatni tekshiramiz
		if state == "edit_anime_photo_waiting" || state == "anime_photo" {
			photos := update.Message.Photo
			photo := photos[len(photos)-1] // Eng sifatli rasm
			code := animeCodeTemp[userID]

			// 1. Rasmni saqlash (Mutex bilan)
			infoMutex.Lock()
			if animePhotos == nil {
				animePhotos = make(map[string]string)
			}
			animePhotos[code] = photo.FileID
			animePhotoMap[code] = photo.FileID
			infoMutex.Unlock()

			// 2. JSONga saqlash
			saveAnimePhotos()
			go saveData()

			// 3. Admin holatini boshqarish
			adminMutex.Lock()
			if state == "anime_photo" {
				// AGAR YANGI ANIME BO'LSA: Videolarga o'tkazamiz
				adminState[userID] = "anime_videos"

				storageMutex.RLock()
				videoCount := len(animeStorage[code])
				storageMutex.RUnlock()

				txt := fmt.Sprintf(
					"🎬 **Nom:** %s\n🆔 **Kod:** `%s`\n\n🌌 **Muqova saqlandi!**\n📊 **Qismlar:** %d ta\n\nEndi **videolarni** yuboring. Tugatgach **/ok** bosing.",
					animeNameTemp[userID], code, videoCount,
				)
				msg := tgbotapi.NewMessage(chatID, txt)
				msg.ParseMode = "Markdown"
				bot.Send(msg)

			} else {
				// AGAR FAQAT TAHRIRLASH BO'LSA: Holatni yopamiz
				adminState[userID] = ""
				bot.Send(tgbotapi.NewMessage(chatID, "✅ Anime muqovasi muvaffaqiyatli yangilandi!"))
			}
			adminMutex.Unlock()

			return // MUHIM: Funksiyadan chiqamiz, pastdagi switch'ga kirmaydi
		}
	} // -------------------------------------

	if state == "waiting_for_ad" || state == "confirm_ad" {
		handleBroadcast(bot, update, adminState, broadcastCache, pendingRequests, &adminMutex, &requestMutex)
		return
	}

	if state == "wait_schedule_time" || state == "wait_schedule_content" {
		handleSchedule(bot, update, state)
		return
	}

	if update.CallbackQuery != nil {
		handleCallback(bot, update)
		return
	}

	if update.Message != nil {
		handleMessage(bot, update)
	}
	switch state {
	case "delete_anime_confirm_final":
		// Bu yerda foydalanuvchi yuborgan text - bu o'chirilishi kerak bo'lgan anime kodi
		code := strings.ToLower(strings.TrimSpace(text))

		// 1. Kod mavjudligini tekshiramiz
		infoMutex.RLock()
		name, exists := animeInfo[code]
		infoMutex.RUnlock()

		if !exists {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod bo‘yicha anime topilmadi. Bekor qilindi."))
			adminMutex.Lock()
			delete(adminState, userID)
			adminMutex.Unlock()
			return
		}

		// 2. 🔥 O'chirib tashlaymiz (Hech qanday /yes so'ramasdan)
		infoMutex.Lock()
		delete(animeInfo, code)
		delete(animePhotos, code)
		delete(animePhotoMap, code)
		infoMutex.Unlock()

		storageMutex.Lock()
		delete(animeStorage, code)
		storageMutex.Unlock()

		// 3. 💾 Ma'lumotlarni saqlaymiz
		go saveData()
		go saveAnimePhotos()

		// 4. Holatni yopamiz va xabar beramiz
		adminMutex.Lock()
		delete(adminState, userID)
		adminMutex.Unlock()

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 '%s' (%s) animeni bazadan butunlay o'chirib tashladim!", name, strings.ToUpper(code))))
		return
	}
}

func playItem(bot *tgbotapi.BotAPI, chatID int64, idx int, lastMessageID int, page int) {
	data := userPages[chatID]
	if data == nil {
		return
	}

	item := data.Items[idx]

	// 1. Tugmalarni yasash (Pagination logic o'sha-o'sha qoladi)
	var keyboard [][]tgbotapi.InlineKeyboardButton
	const pageSize = 9
	start := page * pageSize
	end := start + pageSize
	if end > len(data.Items) {
		end = len(data.Items)
	}

	var row []tgbotapi.InlineKeyboardButton
	for i := start; i < end; i++ {
		qismNo := i + 1
		text := fmt.Sprintf("%d", qismNo)
		if i == idx {
			text = fmt.Sprintf("· %d ·", qismNo) // Tanlangan qism belgisi
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(text, fmt.Sprintf("play_%d_%d", i, page)))
		if len(row) == 3 {
			keyboard = append(keyboard, row)
			row = []tgbotapi.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		keyboard = append(keyboard, row)
	}

	// Navigatsiya (< >)
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️", fmt.Sprintf("page_%d", page-1)))
	}
	if end < len(data.Items) {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("➡️", fmt.Sprintf("page_%d", page+1)))
	}
	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	markup := tgbotapi.NewInlineKeyboardMarkup(keyboard...)

	// 2. MUHIM: Surat tagidagi tugmalarni yangilash (Edit)
	// Bu kod surat o'chib ketmasdan, faqat uning tagidagi tugma o'zgarishini ta'minlaydi
	if lastMessageID != 0 {
		editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, lastMessageID, markup)
		bot.Send(editMsg)
	}

	// 3. Videoni ALOHIDA yangi xabar sifatida yuborish
	caption := fmt.Sprintf(" %s\n %d-qism", data.Name, idx+1)

	switch item.Kind {
	case "video":
		msg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(item.FileID))
		msg.Caption = caption
		bot.Send(msg)
	case "document":
		msg := tgbotapi.NewDocument(chatID, tgbotapi.FileID(item.FileID))
		msg.Caption = caption
		bot.Send(msg)
	}
}

func handleMessage(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	code := strings.ToLower(strings.TrimSpace(update.Message.Text))
	text := update.Message.Text
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	fmt.Printf("--- YANGI XABAR ---\nUser: %d\nText: %s\nState: %s\n------------------\n", userID, text, adminState[userID])
	addUser(userID)
	updateUserActivity(userID)
	// 1. ADMIN PANEL (Adminlar uchun obuna shart emas)
	if text == "/admin" && admins[userID] {
		msg := tgbotapi.NewMessage(chatID, "🛠 Admin panel")
		msg.ReplyMarkup = adminMenu()
		bot.Send(msg)
		return
	}

	// 2. ADMIN STATE HANDLER (Adminlar uchun)
	if admins[userID] {
		handleAdminText(bot, update)
		return
	}

	// 3. BLOKLANISH TEKSHIRUVI
	if blockedUsers[userID] {
		bot.Send(tgbotapi.NewMessage(chatID, "🚫 Siz adminlar tomonidan bloklangansiz."))
		return
	}
	state := adminState[userID]
	if state != "" {

		switch state {

		//case "admin_vip_main":
		//	msg := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "🌟 VIP Foydalanuvchilar Paneli:")
		//	msg.ReplyMarkup = vipAdminMenu()
		//	bot.Send(msg)

		case "wait_delete_code":
			code := strings.ToLower(strings.TrimSpace(text))
			log.Printf("[DEBUG] Admin %d kod yubordi: '%s'", userID, code)

			storageMutex.RLock()
			items, ok := animeStorage[code]
			name, hasName := animeInfo[code]
			storageMutex.RUnlock()

			if !ok || len(items) == 0 {
				log.Printf("[WARNING] Kod topilmadi yoki qismlar bo'sh: '%s'", code)
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday kod topilmadi yoki qismlar yo'q. Qayta yuboring:"))
				return
			}
			if !hasName {
				name = "Nomsiz anime"
			}
			log.Printf("[DEBUG] Anime topildi: '%s', Qismlar soni: %d", name, len(items))

			// Endi holatni o'zgartiramiz
			adminMutex.Lock()
			adminState[userID] = "delete_part_id"
			animeCodeTemp[userID] = code
			adminMutex.Unlock()
			log.Printf("[DEBUG] Admin %d holati 'delete_part_id'ga o'zgartirildi.", userID)

			// Ro'yxatni shakllantirish
			partList := ""
			for i, item := range items {
				partList += fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(item.Kind))
			}

			msgText := fmt.Sprintf("🗑 **%s** (%s)\n\nO'chirmoqchi bo'lgan qism ID'sini kiriting:\n\n%s",
				name, strings.ToUpper(code), partList)

			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"

			_, err := bot.Send(msg)
			if err != nil {
				log.Printf("[ERROR] Xabarni yuborib bo'lmadi: %v", err)
				// Agar Markdown xatosi bo'lsa, oddiy matnda yuborish
				msg.ParseMode = ""
				bot.Send(msg)
			} else {
				log.Printf("[DEBUG] Ro'yxat admin ekraniga chiqarildi.")
			}
			return

		case "delete_part_id":
			code := animeCodeTemp[userID]
			input := strings.TrimSpace(text)
			log.Printf("[DEBUG] O'chirish so'rovi: User: %d, Kod: %s, Kiritilgan IDlar: '%s'", userID, code, input)

			// 1. ID'larni parse qilish
			idsToDelete := parseIDsToDelete(input)
			if len(idsToDelete) == 0 {
				log.Printf("[WARNING] User %d noto'g'ri ID formatini kiritdi: '%s'", userID, input)
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, o'chirmoqchi bo'lgan qism raqamini kiriting (masalan: 1 yoki 1-5):"))
				return
			}
			log.Printf("[DEBUG] Parse qilingan IDlar: %v", idsToDelete)

			// 2. O'chirish jarayoni
			storageMutex.Lock()
			items := animeStorage[code]
			initialCount := len(items)
			log.Printf("[DEBUG] O'chirishdan oldingi qismlar soni: %d", initialCount)

			// ID'larni teskari tartibda saralash (indeks siljib ketmasligi uchun o'ta muhim)
			sort.Slice(idsToDelete, func(i, j int) bool {
				return idsToDelete[i] > idsToDelete[j]
			})
			log.Printf("[DEBUG] Teskari tartibda saralangan IDlar: %v", idsToDelete)

			deletedCount := 0
			for _, id := range idsToDelete {
				indexToRemove := id - 1 // User 1 deydi, bizda 0-indeks

				if indexToRemove >= 0 && indexToRemove < len(items) {
					log.Printf("[DEBUG] O'chirilmoqda: Indeks [%d] (Foydalanuvchi IDsi: %d)", indexToRemove, id)

					// Go usulida elementni slice'dan o'chirish
					items = append(items[:indexToRemove], items[indexToRemove+1:]...)
					deletedCount++
				} else {
					log.Printf("[WARNING] ID %d topilmadi (Mavjud diapazon: 1-%d)", id, len(items))
				}
			}

			// 3. Storage'ni yangilash
			animeStorage[code] = items
			storageMutex.Unlock()

			if deletedCount > 0 {
				log.Printf("[SUCCESS] %s kodli animedan %d ta qism o'chirildi. Qolgan qismlar: %d", code, deletedCount, len(items))
				go saveData() // 💾 JSON faylga saqlash

				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %d ta qism muvaffaqiyatli o'chirildi!", deletedCount)))
			} else {
				log.Printf("[ERROR] Hech qanday qism o'chirilmadi. Kiritilgan IDlar diapazondan tashqarida.")
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Bunday raqamli qismlar topilmadi."))
			}

			// 4. Holatni tozalash
			adminMutex.Lock()
			delete(adminState, userID)
			delete(animeCodeTemp, userID)
			adminMutex.Unlock()
			log.Printf("[DEBUG] User %d holati tozalandi.", userID)
			return

		case "wait_vip_add":
			// 1️⃣ Matnni raqamga (ID) aylantiramiz
			targetID, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato! Iltimos, faqat raqamli ID yuboring."))
				return
			}

			// 2️⃣ VIP muddatini tanlash tugmalarini yuboramiz
			msg := tgbotapi.NewMessage(chatID, "⏳ VIP muddatini tanlang:")
			msg.ReplyMarkup = vipDurationMenu(targetID) // InlineKeyboardMarkup tugmalar bilan
			bot.Send(msg)

			// 3️⃣ Holatni VIP muddat tanlashga o‘zgartiramiz
			adminMutex.Lock()
			adminState[userID] = "wait_vip_duration"
			adminMutex.Unlock()
			return

		case "wait_vip_del":
			targetID, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato! Iltimos, faqat raqamli ID yuboring."))
				return
			}

			vipMutex.Lock()
			if _, exists := vipUsers[targetID]; exists {
				delete(vipUsers, targetID)
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 ID: %d VIP ro'yxatidan o'chirildi!", targetID)))
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu ID VIP ro'yxatida topilmadi."))
			}
			vipMutex.Unlock()

			go saveData()
			delete(adminState, userID)
			return

		case "wait_reorder_ids":
			code := animeCodeTemp[userID]
			input := strings.TrimSpace(text)

			// 1. Foydalanuvchi yuborgan ID-larni massivga olamiz (masalan: [5, 1, 4, 2, 3])
			newOrder := parseIDsToDelete(input)

			storageMutex.Lock()
			oldItems := animeStorage[code]

			if len(newOrder) != len(oldItems) {
				storageMutex.Unlock()
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ Xato! Barcha %d ta IDni kiritishingiz kerak.", len(oldItems))))
				return
			}

			// 2. Yangi tartib bo'yicha vaqtinchalik massiv yaratamiz
			var reorderedItems []ContentItem

			for _, id := range newOrder {
				index := id - 1 // foydalanuvchi yozgan 1 -> index 0
				if index >= 0 && index < len(oldItems) {
					// Tanlangan elementni yangi massivga qo'shamiz
					reorderedItems = append(reorderedItems, oldItems[index])
				} else {
					storageMutex.Unlock()
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Noto'g'ri ID: %d", id)))
					return
				}
			}

			// 3. Bazani yangilaymiz
			animeStorage[code] = reorderedItems
			storageMutex.Unlock()

			go saveData()

			bot.Send(tgbotapi.NewMessage(chatID, "✅ Tartib saqlandi! Endi qismlar siz yuborgan ketma-ketlikda chiqadi."))

			delete(adminState, userID)
			delete(animeCodeTemp, userID)

		case "delete_anime_code":
			code := strings.ToLower(strings.TrimSpace(text))

			infoMutex.RLock()
			_, exists := animeInfo[code]
			infoMutex.RUnlock()

			if !exists {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod bo‘yicha anime topilmadi."))
				delete(adminState, userID)
				return
			}

			// 🔥 O‘chiramiz
			infoMutex.Lock()
			delete(animeInfo, code)
			infoMutex.Unlock()

			storageMutex.Lock()
			delete(animeStorage, code)
			storageMutex.Unlock()

			// 📢 MUHIM: Ma'lumotlar o'chirilgandan so'ng saqlash
			go saveData() // 💾 Qo'shildi

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 '%s' kodi bo‘yicha anime o‘chirildi!", strings.ToUpper(code))))
			delete(adminState, userID)
			return

		case "add_channel_wait":
			text := update.Message.Text
			var chatID64 int64
			var err error

			// 1. ChatID yoki Username orqali kanalni topish
			if strings.HasPrefix(text, "-100") {
				chatID64, err = strconv.ParseInt(text, 10, 64)
			} else {
				username := text
				if !strings.HasPrefix(username, "@") {
					username = "@" + username
				}
				// Username orqali kanal ma'lumotini olish
				chat, err := bot.GetChat(tgbotapi.ChatInfoConfig{
					ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: username},
				})
				if err == nil {
					chatID64 = chat.ID
				}
			}

			if err != nil || chatID64 == 0 {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Kanal topilmadi. ID yoki Usernameni tekshiring."))
				return
			}

			// 2. Kanal turini tekshirish
			chat, _ := bot.GetChat(tgbotapi.ChatInfoConfig{
				ChatConfig: tgbotapi.ChatConfig{ChatID: chatID64},
			})

			if chat.Type == "channel" || chat.Type == "supergroup" {
				// Agar kanal MAXFIY bo'lsa (username yo'q bo'lsa)
				if chat.UserName == "" {
					adminState[userID] = "add_private_link"
					adminTempID[userID] = chat.ID // Kanal ID sini vaqtincha saqlaymiz
					bot.Send(tgbotapi.NewMessage(chatID, "🔒 Bu maxfiy kanal ekan. Iltimos, kanalga ulanish havolasini (Invite Link) yuboring:"))
				} else {
					// Agar kanal OCHIQ bo'lsa
					channels[chat.ID] = chat.UserName
					go saveData()
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Ochiq kanal qo'shildi: @%s", chat.UserName)))
					adminState[userID] = ""
				}
			}

		case "add_admin_id":
			newID, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ ID noto‘g‘ri. Faqat raqam kiriting."))
				return
			}
			admins[newID] = true
			go saveData() // 💾 Qo'shildi
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %d adminlarga qo‘shildi!", newID)))
			delete(adminState, userID)
			return

		case "edit_new_code":
			new_code := strings.ToLower(strings.TrimSpace(text))
			old_code := animeCodeTemp[userID]
			if new_code == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Kod bo‘sh bo‘lishi mumkin emas. Qayta kiriting:"))
				return
			}
			if new_code == old_code {
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Yangi kod eski kod bilan bir xil bo‘lishi mumkin emas."))
				return
			}
			// Yangi kod allaqachon mavjudligini tekshirish
			infoMutex.RLock()
			_, exists := animeInfo[new_code]
			infoMutex.RUnlock()

			if exists {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod (ID) allaqachon boshqa animega tegishli!"))
				return
			}

			// 🔥 Ma'lumotlarni yangi kodga ko'chirish
			infoMutex.Lock()
			storageMutex.Lock()

			// 1. Anime nomini yangi kod bilan saqlash
			animeInfo[new_code] = animeInfo[old_code]

			// 2. Anime kontentini yangi kod bilan saqlash
			animeStorage[new_code] = animeStorage[old_code]

			// 3. Eskilarini o'chirish
			delete(animeInfo, old_code)
			delete(animeStorage, old_code)

			storageMutex.Unlock()
			infoMutex.Unlock()

			// 💾 MUHIM: Ma'lumotlar muvaffaqiyatli ko'chirilgandan so'ng saqlash!
			go saveData() // 💾 Qo'shildi

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ anime kodi  ( %s ) -  dan - ( %s ) ga muvaffaqiyatli o'zgartirildi!", strings.ToUpper(old_code), strings.ToUpper(new_code))))
			delete(adminState, userID)
			delete(animeCodeTemp, userID)
			return // ------------------ ADMIN: Admin o'chirish ID so'rov ------------------

		case "edit_new_name":
			new_name := strings.TrimSpace(text)
			old_code, ok := animeCodeTemp[userID]
			if !ok {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato: anime kodi topilmadi. Qayta boshlash uchun /admin buyrug'ini kiriting."))
				delete(adminState, userID)
				return
			}

			if new_name == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Nom bo‘sh bo‘lishi mumkin emas. Qayta kiriting:"))
				return
			}

			// Nomni yangilash
			infoMutex.Lock()
			animeInfo[old_code] = new_name
			infoMutex.Unlock()

			go saveData() // 💾 Qo'shildi

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ anime nomi ( %s ) ga muvaffaqiyatli o'zgartirildi!", new_name)))

			// Holatni tozalash
			delete(adminState, userID)
			delete(animeCodeTemp, userID)
			return

			// ... yuqoridagi boshqa case'lar (delete_anime_code, add_admin_id va h.k.) ...

			// ⚠️ Kodning boshqa joyida e'lon qilingan bo'lishi kerak:
			// const MAIN_ADMIN_ID int64 = 123456789

		case "remove_admin_id":
			remID, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ ID noto‘g‘ri. Faqat raqam kiriting."))
				return
			}

			if remID == userID {
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ O‘zingizni o‘chirishingiz mumkin emas!"))
				return
			}

			// 👇👇👇 ASOSIY ADMINNI TEKSHIRISH QO'SHILDI 👇👇👇
			if remID == MAIN_ADMIN_ID {
				bot.Send(tgbotapi.NewMessage(chatID, "🛑 Asosiy adminni o‘chirish mumkin emas!"))
				return
			}
			// 👆👆👆 ASOSIY ADMINNI TEKSHIRISH QO'SHILDI 👆👆👆

			delete(admins, remID)
			go saveData() // 💾 Qo'shildi
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 %d admin o‘chirildi!", remID)))
			delete(adminState, userID)
			return // return qo'shildi

		case "block_user":
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato ID. Raqam kiriting."))
				return
			}
			blockedUsers[id] = true
			go saveData() // 💾 Qo'shildi
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %d bloklandi!", id)))
			delete(adminState, userID)
			return // return qo'shildi

		case "unblock_user":
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato ID. Raqam kiriting."))
				return
			}
			delete(blockedUsers, id)
			go saveData() // 💾 Qo'shildi
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("♻️ %d blokdan chiqarildi!", id)))
			delete(adminState, userID)
			return // return qo'shildi

		case "add_channel_chatid":
			text = strings.TrimSpace(text)

			// 1️⃣ HAVOLA bo‘lsa → bu ID EMAS
			if strings.Contains(text, "t.me") {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu chatID emas. Iltimos, kanalning raqamli ChatID'sini yuboring (masalan: -1001234567890)."))
				return
			}

			// 2️⃣ faqat raqam bo‘lishi kerak va -100 bilan boshlanishi sharti
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil || !strings.HasPrefix(text, "-100") {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Noto‘g‘ri chatID. ChatID -100 bilan boshlanishi kerak."))
				return
			}

			// 3️⃣ ChatID vaqtinchalik saqlanadi
			adminTempID[userID] = id
			adminState[userID] = "add_channel_username"

			bot.Send(tgbotapi.NewMessage(chatID, "🔗 Endi kanal username yoki havolasini yuboring:"))
			return

		case "add_channel_username":
			username := strings.TrimSpace(text)

			if username == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, kanal username yoki havolasini kiriting!"))
				return
			}

			chatIDnum := adminTempID[userID]

			// Maxfiy havola bo'lsa
			if strings.HasPrefix(username, "https://t.me/+") {
				// Bot kanalga kirmaydi, faqat ro'yxatga qo'shamiz
				adminTempChannels[userID] = append(adminTempChannels[userID], Channel{
					ChatID:   0, // hali raqam yo'q
					Username: username,
				})

				bot.Send(tgbotapi.NewMessage(chatID, "🔗 Guruh havolasi olindi!\n📨 Admin tasdiqlashi kerak."))
				bot.Send(tgbotapi.NewMessage(chatID, "✅ Kanal qo‘shildi!"))
			} else {
				// Oddiy username yoki raqamli ChatID
				channels[chatIDnum] = username
				saveData()
				bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("✅ Kanal qo‘shildi!\nChatID: %d\nUsername: %s", chatIDnum, username),
				))
			}

			delete(adminState, userID)
			delete(adminTempID, userID)
			return

		case "remove_channel":
			if strings.HasPrefix(text, "https://t.me/+") {
				// Maxfiy havola bo'lsa
				// ... (Sizning kodingizdagi kabi, adminTempChannels ro'yxatidan o'chirish)
				found := false
				for i, ch := range adminTempChannels[userID] {
					if ch.Username == text {
						adminTempChannels[userID] = append(adminTempChannels[userID][:i], adminTempChannels[userID][i+1:]...)
						found = true
						bot.Send(tgbotapi.NewMessage(chatID, "✅ Maxfiy kanal havolasi o‘chirildi!")) // Xabarni aniqlashtirdik
						break
					}
				}
				if !found {
					bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday maxfiy havola topilmadi."))
				}
				return
			}

			// Raqamli ChatID bo'lsa
			id, err := strconv.ParseInt(text, 10, 64)
			if err == nil { // Matn ChatID raqami sifatida parse qilina olsa

				// 1. Oddiy kanallar xaritasidan o'chirish (agar mavjud bo'lsa)
				if _, ok := channels[id]; ok {
					delete(channels, id)
					saveData()
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Oddiy kanal o‘chirildi! \n(ChatID: %d)", id)))
					return
				}

				// 2. adminTempChannels ro'yxatidan o'chirish (agar maxfiy kanal bo'lsa)
				found := false
				for i, ch := range adminTempChannels[userID] {
					// Agar ro'yxatdagi kanalning ChatID'si so'ralgan ID ga teng bo'lsa
					if ch.ChatID == id {
						adminTempChannels[userID] = append(adminTempChannels[userID][:i], adminTempChannels[userID][i+1:]...)
						found = true
						bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Ro'yxatdagi kanal o‘chirildi! \n (ChatID: %d) ", id)))
						break
					}
				}
				if found {
					return
				}

			}

			// Agar raqam bo'lmasa yoki yuqorida topilmagan bo'lsa, username bo'lishi mumkin
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday kanal topilmadi. Raqamli ChatID yoki to'liq maxfiy havolani kiriting."))
			return

		case "anime_name":
			name := strings.TrimSpace(text)
			if name == "" {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Nomi bo'sh bo'lishi mumkin emas. Iltimos nom kiriting:"))
				return
			}
			animeNameTemp[userID] = name
			adminState[userID] = "anime_code"
			bot.Send(tgbotapi.NewMessage(chatID, "🆔 Endi anime kodi kiriting:"))
			return

		case "delete_channel":
			text = strings.TrimSpace(text)
			var found bool
			var chatID int64

			// 1. Agar raqam bo'lsa
			id, err := strconv.ParseInt(text, 10, 64)
			if err == nil {
				if _, ok := channels[id]; ok {
					delete(channels, id)
					found = true
					chatID = id
				}
			} else {
				// 2. Agar havola (@username) bo'lsa
				for idTemp, username := range channels {
					if username == text || "@"+username == text {
						delete(channels, idTemp)
						found = true
						chatID = idTemp
						break
					}
				}
			}

			// Javob yuborish
			if found {
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Kanal o‘chirildi! ChatID: %d", chatID)))
			} else {
				// Agar topilmasa, javobni adminga yoki default chatga yuborish kerak
				bot.Send(tgbotapi.NewMessage(chatID /* yoki defaultChatID */, "❌ Bunday ChatID yoki havola topilmadi."))
			}
			return

		case "anime_code":
			code := strings.ToLower(strings.TrimSpace(text))
			userID := update.Message.From.ID

			// 1️⃣ TEKSHIRUV: Bazani o'qiymiz (loadData yuklab qo'ygan ma'lumot ichidan)
			infoMutex.RLock()
			existingName, exists := animeInfo[code]
			infoMutex.RUnlock()

			if exists {
				// 🛑 AGAR KOD BAND BO'LSA:
				msg := fmt.Sprintf("❌ Kechirasiz, bu kod (%s) band!\n🎬 Anime: %s\n\nIltimos, boshqa kod yuboring:", code, existingName)
				bot.Send(tgbotapi.NewMessage(chatID, msg))
				return // Keyingi qadamga o'tmaydi!
			}

			// 2️⃣ AGAR KOD BO'SH BO'LSA - ENDI SAQLAYMIZ
			infoMutex.Lock()
			animeInfo[code] = animeNameTemp[userID]
			infoMutex.Unlock()

			animeCodeTemp[userID] = code
			adminState[userID] = "anime_photo"
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Kod qabul qilindi. Endi muqova (rasm) yuboring:"))
			return

		case "anime_photo":
			// 1. Faqat rasm ekanligini tekshiramiz
			if update.Message == nil || update.Message.Photo == nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, anime muqovasi uchun rasm yuboring:"))
				return
			}

			// 2. Rasmni olamiz
			photoArray := update.Message.Photo
			photoID := photoArray[len(photoArray)-1].FileID
			code := animeCodeTemp[userID]

			// 3. Rasmni mapga saqlaymiz
			infoMutex.Lock()
			if animePhotos == nil {
				animePhotos = make(map[string]string)
			}
			animePhotos[code] = photoID
			infoMutex.Unlock()

			// 4. Holatni videolarga o'zgartiramiz
			adminState[userID] = "anime_videos"

			storageMutex.RLock()
			videoCount := len(animeStorage[code])
			storageMutex.RUnlock()

			// 5. Siz xohlagan xabar
			txt := fmt.Sprintf(
				"🎬 **Nom:** %s\n🆔 **Kod:** `%s`\n\n🌌 **Muqova alohida saqlandi!**\n📊 **Hozirgi qismlar:** %d ta\n\nEndi **video yoki fayllarni** yuboring. Tugatgach **/ok** deb yozing.",
				animeNameTemp[userID], code, videoCount,
			)

			msg := tgbotapi.NewMessage(chatID, txt)
			msg.ParseMode = "Markdown"
			bot.Send(msg)

			go saveData()
			return

		case "add_private_link":
			link := update.Message.Text
			if !strings.HasPrefix(link, "https://t.me/") {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Noto'g'ri havola. Havola https://t.me/ bilan boshlanishi kerak."))
				return
			}

			targetChatID := adminTempID[userID]
			channels[targetChatID] = link // Maxfiy kanal uchun siz yuborgan havolani saqlaydi
			go saveData()

			bot.Send(tgbotapi.NewMessage(chatID, "✅ Maxfiy kanal va havola muvaffaqiyatli saqlandi!"))
			adminState[userID] = ""
			delete(adminTempID, userID)

		case "edit_anime_code":
			code := strings.ToLower(strings.TrimSpace(text))

			infoMutex.RLock()
			name, exists := animeInfo[code]
			infoMutex.RUnlock()

			if !exists {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod bo‘yicha anime topilmadi."))
				delete(adminState, userID)
				return
			}

			// 🔥 Qismlar sonini hisoblash
			storageMutex.RLock()
			partCount := len(animeStorage[code])
			storageMutex.RUnlock()

			// ✅ Menyuni yuborish
			msg := tgbotapi.NewMessage(
				chatID,
				fmt.Sprintf("anime: %s (%s)\nMavjud qismlar soni: %d ta.\n\nQuyidagilardan birini tanlang:",
					name, strings.ToUpper(code), partCount), // <-- Qismlar soni qo'shildi
			)
			msg.ParseMode = "Markdown"

			// editMenu ni chaqirish
			msg.ReplyMarkup = editMenu(code, name)

			delete(adminState, userID)
			bot.Send(msg)
			return // ------------------ ADMIN: kontent qabul qilish (TO'G'RILANGAN) ------------------
		// handleAdminText funksiyasi ichidagi switch blokiga qo'shiladi

		// handleAdminText funksiyasi ichidagi switch blokida "anime_videos" holatining yangilangan qismi:
		case "anime_videos":
			code := animeCodeTemp[userID]
			chatID := update.Message.Chat.ID
			text := update.Message.Text

			// 1. Dastlabki tekshiruv va Log
			if code == "" {
				log.Printf("⚠️ [XATO] User %d: Anime kodi topilmadi (State: anime_videos)", userID)
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xatolik: anime kodi topilmadi."))
				delete(adminState, userID)
				return
			}

			// 2. /ok — yuklashni yakunlash
			if strings.ToLower(text) == "/ok" {
				storageMutex.RLock()
				total := len(animeStorage[code])
				storageMutex.RUnlock()

				// TERMINAL LOG
				fmt.Printf("\n--- ✅ YAKUNLASH ---\n")
				fmt.Printf("📂 Kod: %s\n📦 Jami qismlar: %d\n👤 Admin: %d\n-------------------\n", code, total, userID)

				go saveData()

				link := fmt.Sprintf("https://t.me/ergergtr_bot?start=%s", code)
				finishText := fmt.Sprintf(" '%s' uchun yuklash tugadi!\n\n Jami: %d ta qism\n Ko‘rish: %s",
					animeNameTemp[userID], total, link)

				bot.Send(tgbotapi.NewMessage(chatID, finishText))

				// Tozalash logi
				delete(adminState, userID)
				delete(animeCodeTemp, userID)
				delete(animeNameTemp, userID)
				log.Printf("🧹 [INFO] User %d uchun sessiya tozalandi.", userID)
				return
			}

			// 3. Kontentni aniqlash
			var item ContentItem
			var itemKind string
			var isContent = true

			// ... (formatlarni tekshirish qismi o'zgarishsiz qoladi) ...
			// ... video, photo, document, text ...

			// 4. Kontentni saqlash va Terminalda ko'rsatish
			if isContent {
				storageMutex.Lock()
				animeStorage[code] = append(animeStorage[code], item)
				episode := len(animeStorage[code])
				storageMutex.Unlock()

				// TERMINAL LOG (Har bir qism kelganda)
				log.Printf("📥 [YANGI QISIM] Kod: %s | Qism: %d | Turi: %s", code, episode, itemKind)

				msg := fmt.Sprintf("💾 %d-qism qabul qilindi. Turi: %s /ok ", episode, strings.Title(itemKind))
				bot.Send(tgbotapi.NewMessage(chatID, msg))
			}
			return
		//case "anime_videos":
		//	// 1. Dastlabki tekshiruvlar va o'zgaruvchilar
		//	code := animeCodeTemp[userID]
		//	chatID := update.Message.Chat.ID
		//	text := update.Message.Text
		//
		//	// Agar anime kodi bo'lmasa — xato
		//	if code == "" {
		//		bot.Send(tgbotapi.NewMessage(
		//			chatID,
		//			"❌ Xatolik: anime kodi topilmadi. Jarayonni boshidan boshlang.",
		//		))
		//		delete(adminState, userID)
		//		return
		//	}
		//
		//	// 2. /ok — yuklashni yakunlash
		//	if strings.ToLower(text) == "/ok" {
		//		storageMutex.RLock()
		//		total := len(animeStorage[code])
		//		storageMutex.RUnlock()
		//
		//		// Asinxron saqlash
		//		go saveData()
		//
		//		// 🔗 Deep link
		//		link := fmt.Sprintf("https://t.me/ergergtr_bot?start=%s", code)
		//
		//		// Yakuniy xabar
		//		finishText := fmt.Sprintf(
		//			" '%s' uchun yuklash tugadi!\n\n"+
		//				" Jami: %d ta qism\n"+
		//				" Ko‘rish: %s",
		//			animeNameTemp[userID],
		//			total,
		//			link,
		//		)
		//
		//		bot.Send(tgbotapi.NewMessage(chatID, finishText))
		//
		//		// Tozalash
		//		delete(adminState, userID)
		//		delete(animeCodeTemp, userID)
		//		delete(animeNameTemp, userID)
		//		return
		//	}
		//
		//	// 3. Kontentni aniqlash
		//	var item ContentItem
		//	var itemKind string
		//	var isContent = true
		//
		//	if update.Message.Video != nil {
		//		item = ContentItem{
		//			Kind:   "video",
		//			FileID: update.Message.Video.FileID,
		//		}
		//		itemKind = "video"
		//
		//	} else if update.Message.Photo != nil {
		//		p := update.Message.Photo[len(update.Message.Photo)-1]
		//		item = ContentItem{
		//			Kind:   "photo",
		//			FileID: p.FileID,
		//		}
		//		itemKind = "rasm"
		//
		//	} else if update.Message.Document != nil {
		//		item = ContentItem{
		//			Kind:   "document",
		//			FileID: update.Message.Document.FileID,
		//		}
		//		itemKind = "fayl"
		//
		//	} else if update.Message.Text != "" {
		//		item = ContentItem{
		//			Kind: "text",
		//			Text: update.Message.Text,
		//		}
		//		itemKind = "matn"
		//
		//	} else {
		//		isContent = false
		//		bot.Send(tgbotapi.NewMessage(
		//			chatID,
		//			"❌ Noma’lum format! Video, rasm, fayl yoki matn yuboring.",
		//		))
		//		return
		//	}
		//
		//	// 4. Kontentni saqlash
		//	if isContent {
		//		storageMutex.Lock()
		//
		//		animeStorage[code] = append(animeStorage[code], item)
		//		episode := len(animeStorage[code])
		//
		//		storageMutex.Unlock()
		//
		//		// Adminni xabardor qilish
		//		msg := fmt.Sprintf(
		//			"💾 %d-qism qabul qilindi. Turi: %s /ok ",
		//			episode,
		//			strings.Title(itemKind),
		//		)
		//
		//		bot.Send(tgbotapi.NewMessage(chatID, msg))
		//	}
		//
		//	return

		case "broadcast_text":
			broadcastCache[userID] = update.Message // <-- *tgbotapi.Message sifatida saqlaymiz

			bot.Send(tgbotapi.NewMessage(chatID, "⬆️ Reklama tayyor. Hammasi to'g'rimi? (Ha / Yo‘q)"))
			adminState[userID] = "broadcast_confirm"
			return
		case "broadcast_confirm":
			if strings.ToLower(strings.TrimSpace(text)) == "ha" {
				go func(msg *tgbotapi.Message) {
					for id := range allUsers {
						if msg.Text != "" {
							bot.Send(tgbotapi.NewMessage(id, msg.Text))
						} else if msg.Photo != nil {
							photo := msg.Photo[len(msg.Photo)-1]
							bot.Send(tgbotapi.NewPhoto(id, tgbotapi.FileID(photo.FileID)))
						} else if msg.Video != nil {
							bot.Send(tgbotapi.NewVideo(id, tgbotapi.FileID(msg.Video.FileID)))
						} else if msg.Document != nil {
							bot.Send(tgbotapi.NewDocument(id, tgbotapi.FileID(msg.Document.FileID)))
						}
					}
				}(broadcastCache[userID])

				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📢 Reklama barcha foydalanuvchilarga yetkazildi! (%d ta)", len(allUsers))))
			} else {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Reklama bekor qilindi."))
			}

			delete(broadcastCache, userID)
			delete(adminState, userID)
			return

		default:
			// Agar 'adminState' o'rnatilgan bo'lsa, lekin 'case' mos kelmasa
			bot.Send(tgbotapi.NewMessage(chatID, "❓ Noma'lum holat! Iltimos, qayta urining yoki /admin deb yozing."))
			delete(adminState, userID) // Noto'g'ri holatni tozalash
			return
		}

		return

	}

	// ---------------------------------------------------------
	// 🔥 MAJBURIY OBUNA TEKSHIRUVI (ASOSIY O'ZGARISH SHU YERDA)
	// ---------------------------------------------------------
	isMember, requiredChannelsMap := checkMembership(bot, userID, true)
	if !isMember {
		handleMembershipCheck(bot, chatID, requiredChannelsMap)
		return // Foydalanuvchi obuna bo'lmaguncha pastdagi kodlarga o'tmaydi
	}
	// ---------------------------------------------------------

	updateUserActivity(userID)

	// 4. /start BUYRUG'I
	if update.Message != nil && update.Message.IsCommand() && update.Message.Command() == "start" {
		chatID := update.Message.Chat.ID

		// 1. Argumentni olish (Deep-link: /start 1 bo'lsa, code = "1" bo'ladi)
		code := update.Message.CommandArguments()

		// Agar argument bo'sh bo'lsa (shunchaki /start bosilsa)
		if code == "" {
			msg := tgbotapi.NewMessage(chatID, "👋 Assalomu alaykum!\n\n🔎 Anime olish uchun kod kiriting:")
			msg.ReplyMarkup = userMenu // Sizdagi asosiy menyu
			bot.Send(msg)
			return
		}

		// 2. Bazadan ma'lumotni qidirish
		storageMutex.RLock()
		items, ok := animeStorage[code]
		storageMutex.RUnlock()

		if ok && len(items) > 0 {
			// --- ANIMENI TAYYORLASH ---
			name := items[0].Text
			if name == "" {
				name = "Anime (Kod: " + code + ")"
			}

			userPages[chatID] = &UserPage{
				Name:  name,
				Items: items,
				Page:  0,
			}

			// 3. SIZNING FORMATINGIZ BO'YICHA CHIQARISH
			markup := sendPageMenuMarkup(chatID)
			caption := fmt.Sprintf("Anime nomi: %s\nJami qismlar: %d ta\n\n🍿 Tomosha qilish uchun kerakli qisimni tanlang:", name, len(items))

			// Muqova rasmini tekshirish
			infoMutex.RLock()
			pID, hasPhoto := animePhotos[code]
			infoMutex.RUnlock()

			if hasPhoto && pID != "" {
				// ✅ MUQOVA RASMI BILAN
				msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(pID))
				msg.Caption = caption
				msg.ParseMode = "Markdown"
				msg.ReplyMarkup = markup
				bot.Send(msg)
			} else {
				// ⚠️ RASM BO'LMASA - BIRINCHI QISMNI YUBORISH
				firstItem := items[0]
				qismCaption := fmt.Sprintf("🎬 %s\n🔢 Qism: 1/%d", name, len(items))

				switch firstItem.Kind {
				case "video":
					msg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(firstItem.FileID))
					msg.Caption = qismCaption
					msg.ReplyMarkup = markup
					bot.Send(msg)
				case "document":
					msg := tgbotapi.NewDocument(chatID, tgbotapi.FileID(firstItem.FileID))
					msg.Caption = qismCaption
					msg.ReplyMarkup = markup
					bot.Send(msg)
				default:
					msg := tgbotapi.NewMessage(chatID, caption)
					msg.ReplyMarkup = markup
					bot.Send(msg)
				}
			}
		} else {
			// Kod topilmasa
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Kechirasiz, bu kod bo'yicha hech narsa topilmadi."))
		}
		return
	}
	if text == "/stats" {
		displayStats(bot, chatID)
		return
	}
	// Xabar forward qilinganini tekshirish
	if update.Message.ForwardFromChat != nil {
		fChat := update.Message.ForwardFromChat
		if fChat.Type == "channel" || fChat.Type == "supergroup" {

			// Agar kanal maxfiy bo'lsa (Username yo'q bo'lsa)
			if fChat.UserName == "" {
				adminState[userID] = "add_private_link"
				adminTempID[userID] = fChat.ID // <--- MUHIM: ID-ni bu yerda saqlaymiz

				bot.Send(tgbotapi.NewMessage(chatID, "🔒 Bu maxfiy kanal. Endi kanalga ulanish havolasini (Invite Link) yuboring:"))
			} else {
				// Ochiq kanal bo'lsa
				channels[fChat.ID] = fChat.UserName
				go saveData()
				bot.Send(tgbotapi.NewMessage(chatID, "✅ Ochiq kanal qo'shildi: @"+fChat.UserName))
			}
			return
		}
	}
	// 5. MAXSUS BUYRUQLAR (Masalan: /clear_channels)
	switch text {
	case "/clear_channels":
		if !adminIDs[userID] {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Sizda bu buyruqni ishlatish huquqi yo‘q."))
			return
		}
		channels = make(map[int64]string)
		go saveData()
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Barcha kanallar o‘chirildi!"))
		return
	}

	// 6. KOD KIRITSA (FOYDALANUVCHI)
	// Bu yerda text != "/start" tekshiruvi shart emas, chunki tepada return qilingan
	code = strings.ToLower(strings.TrimSpace(update.Message.Text)) // ✅ Faqat "="	// 1. Tugma bosilganda qidiruv rejimini yoqish
	if update.Message.Text == "🔍 Rasm/Video orqali qidirish" {
		userState[userID] = "wait_search"
		msg := tgbotapi.NewMessage(chatID, "🔍 Iltimos, qidirmoqchi bo'lgan **rasmingizni** yoki **videongizni** yuboring...")
		msg.ParseMode = "markdown"
		bot.Send(msg)
		return
	}

	// 2. Qidiruv rejimida rasm yoki video kelganini tekshirish
	if update.Message != nil && userState[userID] == "wait_search" {
		var finalFileID string

		// A. AGAR RASM YUBORILSA
		if update.Message.Photo != nil {
			photos := update.Message.Photo
			finalFileID = photos[len(photos)-1].FileID // eng sifatli rasm

		} else if update.Message.Video != nil {
			video := update.Message.Video

			loadingMsg, _ := bot.Send(
				tgbotapi.NewMessage(chatID, "⏳ Video tahlil qilinmoqda (o'rtasidan kadr olinmoqda)..."),
			)

			tempVideo := fmt.Sprintf("temp_%d.mp4", userID)
			tempImg := fmt.Sprintf("out_%d.jpg", userID)

			// 1. Videoni yuklab olish
			file, err := bot.GetFile(tgbotapi.FileConfig{FileID: video.FileID})
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Video fayl topilmadi."))
				return
			}
			//
			downloadURL := fmt.Sprintf(
				"https://api.telegram.org/file/bot%s/%s",
				bot.Token,
				file.FilePath,
			)

			if err := downloadFile(downloadURL, tempVideo); err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Videoni yuklab bo‘lmadi."))
				return
			}

			defer os.Remove(tempVideo)
			defer os.Remove(tempImg)

			// 2. O‘rtasidan kadr olish
			half := video.Duration / 2

			// ⚠️ MUHIM: -ss ni -i dan OLDIN qo‘yish (tez va barqaror)
			cmd := exec.Command(
				"ffmpeg",
				"-ss", strconv.Itoa(half),
				"-i", tempVideo,
				"-frames:v", "1",
				"-q:v", "2",
				tempImg,
				"-y",
			)

			if err := cmd.Run(); err != nil {
				bot.Send(tgbotapi.NewMessage(
					chatID,
					"❌ FFmpeg xatosi yuz berdi.\n\n"+
						"🔧 Serverda ffmpeg o‘rnatilganini tekshiring.",
				))
				return
			}

			// 3. Rasmni Telegram’ga yuklash
			photoMsg, err := bot.Send(
				tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(tempImg)),
			)
			if err == nil {
				finalFileID = photoMsg.Photo[len(photoMsg.Photo)-1].FileID
			}

			// loading xabarini o‘chirish
			bot.Send(tgbotapi.NewDeleteMessage(chatID, loadingMsg.MessageID))

		} else {
			bot.Send(tgbotapi.NewMessage(
				chatID,
				"⚠️ Iltimos, faqat rasm yoki video yuboring!",
			))
			return
		}

		// --- Google Lens havolasi ---
		if finalFileID != "" {
			file, _ := bot.GetFile(tgbotapi.FileConfig{FileID: finalFileID})
			imageURL := fmt.Sprintf(
				"https://api.telegram.org/file/bot%s/%s",
				bot.Token,
				file.FilePath,
			)

			googleURL := "https://lens.google.com/uploadbyurl?url=" +
				url.QueryEscape(imageURL)

			inlineKb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonURL(
						"🔍 Natijani ko‘rish (Google Lens)",
						googleURL,
					),
				),
			)

			msg := tgbotapi.NewMessage(
				chatID,
				"✅ Markaziy kadr olindi. Qidirish uchun bosing:",
			)
			msg.ReplyMarkup = inlineKb
			bot.Send(msg)
		}

		delete(userState, userID)
		return
	}
	// Qidruv statistikasi
	statsMutex.Lock()
	searchStats[code]++
	statsMutex.Unlock()

	// Saqlangan kontentni o'qish
	storageMutex.RLock()
	items, ok := animeStorage[code]
	storageMutex.RUnlock()

	if !ok || len(items) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "🔍 Bunday kod bo‘yicha kontent topilmadi."))
		return
	}

	// Nomni olish
	infoMutex.RLock()
	name, hasName := animeInfo[code]
	infoMutex.RUnlock()
	if !hasName {
		name = "No-name"
	}

	// Pagination uchun ma'lumotni saqlash
	// ... (tepadagi kodlar o'zgarishsiz qoladi) ...

	// Pagination uchun ma'lumotni saqlash
	userPages[chatID] = &UserPage{
		Name:  name,
		Items: items,
		Page:  0,
	}

	if len(items) > 0 {
		// 1. Tugmalarni yasaymiz
		markup := sendPageMenuMarkup(chatID)

		// 2. Sarlavha matni
		caption := fmt.Sprintf("Anime nomi: %s\nJami qismlar: %d ta\n\n🍿 Tomosha qilish uchun kerakli qisimni tanlang :", name, len(items))

		// 3. 📸 MUQOVA RASMINI OLISH (Lock ichida)
		infoMutex.RLock()
		// DIQQAT: Agar map nomi 'animePhotoMap' bo'lsa, o'shani yozing!
		pID, hasPhoto := animePhotos[code]
		infoMutex.RUnlock()

		if hasPhoto && pID != "" {
			// ✅ AGAR MUQOVA RASMI BO'LSA - O'SHANI YUBORAMIZ
			msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(pID))
			msg.Caption = caption
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = markup
			bot.Send(msg)
		} else {
			// ⚠️ AGAR MUQOVA YO'Q BO'LSA - BIRINCHI QISMNI YUBORAMIZ
			firstItem := items[0]
			qismCaption := fmt.Sprintf("🎬 %s\n🔢 Qism: 1/%d", name, len(items))

			switch firstItem.Kind {
			case "video":
				msg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(firstItem.FileID))
				msg.Caption = qismCaption
				msg.ReplyMarkup = markup
				bot.Send(msg)
			case "document":
				msg := tgbotapi.NewDocument(chatID, tgbotapi.FileID(firstItem.FileID))
				msg.Caption = qismCaption
				msg.ReplyMarkup = markup
				bot.Send(msg)
			default:
				msg := tgbotapi.NewMessage(chatID, caption)
				msg.ReplyMarkup = markup
				bot.Send(msg)
			}
		}
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Kontent topildi, lekin qismlar hali yuklanmagan."))
	}
}

func downloadFile(fileURL string, filepath string) error {
	// 1. URL orqali faylga so'rov yuboramiz
	resp, err := http.Get(fileURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 2. Kompyuterda bo'sh fayl yaratamiz
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// 3. Internetdan kelayotgan ma'lumotni faylga ko'chiramiz
	_, err = io.Copy(out, resp.Body)
	return err
}

func adminMenu() tgbotapi.InlineKeyboardMarkup {

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(

			tgbotapi.NewInlineKeyboardButtonData("📊 Statistika", "show_stats"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👮‍♂️ Adminlar", "admin_menu"),
		),

		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Foydalanuvchilar", "user_menu"),
		),
		tgbotapi.NewInlineKeyboardRow(

			tgbotapi.NewInlineKeyboardButtonData("✍️ anime tahrirlash", "edit_anime"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📢 Reklama yuborish", "broadcast"),
			tgbotapi.NewInlineKeyboardButtonData("⏰ Rejalashtirilgan post", "admin_schedule"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🖋 anime joylash", "upload_anime"),
			tgbotapi.NewInlineKeyboardButtonData("🗑 anime o‘chirish", "delete_anime"),
		),

		tgbotapi.NewInlineKeyboardRow(

			tgbotapi.NewInlineKeyboardButtonData("➕ Kanal qo‘shish", "add_channel"),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Kanal o‘chirish", "remove_channel"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎬 Barcha kinolar", "all_animes"),
			tgbotapi.NewInlineKeyboardButtonData("🛠 Maxsus buyruqlar", "special_commands"),
		),
	)

}

func userManageKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Foydalanuvchini bloklash", "block_user"),
			tgbotapi.NewInlineKeyboardButtonData("♻️ Blokdan chiqarish", "unblock_user"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📵 Bloklanganlar ro‘yxati", "blocked_list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ VIP Qo'shish", "vip_add"),
			tgbotapi.NewInlineKeyboardButtonData("🗑 VIP O'chirish", "vip_del"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📜 VIP Ro'yxati", "vip_list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Orqaga", "back_to_admin"),
		),
	)
}

func adminManageKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👤 Admin qo‘shish", "add_admin"),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Adminni o‘chirish", "remove_admin"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Adminlar ro‘yxati", "list_admins"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Orqaga", "back_to_admin"),
		),
	)
}

func startVIPChecker() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			now := time.Now()

			vipMutex.Lock()
			changed := false

			for userID, vip := range vipUsers {
				if vip.ExpireAt.Before(now) {
					delete(vipUsers, userID)
					log.Printf("❌ VIP muddati tugadi: %d", userID)
					changed = true
				}
			}

			vipMutex.Unlock()

			if changed {
				saveData()
			}
		}
	}()
}

func sendVIPList(bot *tgbotapi.BotAPI, chatID int64, messageID int, page int) {
	vipMutex.RLock()
	defer vipMutex.RUnlock()

	if len(vipUsers) == 0 {
		msg := tgbotapi.NewEditMessageText(chatID, messageID, "🌟 VIP foydalanuvchilar yo‘q.")
		bot.Send(msg)
		return
	}

	// VIPlarni slice ga yig‘amiz
	var list []VIPUser
	now := time.Now()
	for _, v := range vipUsers {
		if v.ExpireAt.After(now) {
			list = append(list, v)
		}
	}

	start := page * vipPerPage
	end := start + vipPerPage

	if start < 0 {
		start = 0
	}
	if start >= len(list) {
		return
	}
	if end > len(list) {
		end = len(list)
	}

	// ✅ ODDIY MATN (JSON/HTML YO‘Q)
	text := "🌟 VIP foydalanuvchilar ro'yxati:\n\n"

	for _, vip := range list[start:end] {
		remain := time.Until(vip.ExpireAt)

		days := int(remain.Hours()) / 24
		hours := int(remain.Hours()) % 24

		text += fmt.Sprintf(
			"👤 %d\n⏳ Qolgan vaqt: %d kun %d soat\n\n",
			vip.UserID, days, hours,
		)
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)

	// 🔘 Tugmalar
	var row []tgbotapi.InlineKeyboardButton

	if page > 0 {
		row = append(row,
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Oldingi", fmt.Sprintf("vip_list:%d", page-1)),
		)
	}
	if end < len(list) {
		row = append(row,
			tgbotapi.NewInlineKeyboardButtonData("➡️ Keyingi", fmt.Sprintf("vip_list:%d", page+1)),
		)
	}

	if len(row) > 0 {
		markup := tgbotapi.NewInlineKeyboardMarkup(row)
		edit.ReplyMarkup = &markup
	}

	bot.Send(edit)
}

func vipDurationMenu(targetID int64) *tgbotapi.InlineKeyboardMarkup {
	markup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"7 kun",
				fmt.Sprintf("vip_duration:%d:7d", targetID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				"30 kun",
				fmt.Sprintf("vip_duration:%d:30d", targetID),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"6 oy",
				fmt.Sprintf("vip_duration:%d:6m", targetID),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				"1 yil",
				fmt.Sprintf("vip_duration:%d:1y", targetID),
			),
		),
	)
	return &markup
}

func editMenu(code, name string) *tgbotapi.InlineKeyboardMarkup {

	markup := tgbotapi.NewInlineKeyboardMarkup(

		tgbotapi.NewInlineKeyboardRow(

			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("✏️ Nomini o‘zgartirish (%s)", name), "edit_name:"+code),
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("🆔 Kodini o‘zgartirish (%s)", code), "edit_code:"+code),
		),

		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Qo‘shish", "edit_content:"+code),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Qismni o'chirish", "delete_part:"+code),
		),

		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌌 suratni o'zgartirish", "edit_photo:"+code),
			tgbotapi.NewInlineKeyboardButtonData("🔢 Tartiblash", "reorder_request:"+code), // Yangi tugma
		),
		// Kontent tahrirlash
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ anime to'plamini o'chirish", "delete_anime_confirm:"+code),
		),
	)

	return &markup

}

func checkMembership(bot *tgbotapi.BotAPI, userID int64, isCallback bool) (bool, map[int64]string) {
	missing := make(map[int64]string)

	requestMutex.Lock()
	userRequests := pendingRequests[userID] // Foydalanuvchi yuborgan barcha so'rovlar
	requestMutex.Unlock()

	for chatID, info := range channels {
		member, _ := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: chatID,
				UserID: userID,
			},
		})

		isMember := member.Status == "member" ||
			member.Status == "administrator" ||
			member.Status == "creator"

		// MUHIM QISM:
		// Agar a'zo bo'lmasa VA aynan shu chatID ga so'rov yubormagan bo'lsa
		if !isMember && (userRequests == nil || !userRequests[chatID]) {
			missing[chatID] = info
		}
	}

	return len(missing) == 0, missing
}

func saveRequests() {
	requestMutex.RLock()
	defer requestMutex.RUnlock()
	file, _ := json.Marshal(pendingRequests)
	_ = os.WriteFile("requests.json", file, 0644)
}

func loadRequests() {
	file, err := os.ReadFile("requests.json")
	if err == nil {
		json.Unmarshal(file, &pendingRequests)
	}
}

func handleMembershipCheck(bot *tgbotapi.BotAPI, chatID int64, missing map[int64]string) {
	fmt.Printf("\n--- 🧱 Tugmalar Yasash Boshlandi (ChatID: %d) ---\n", chatID)

	text := "<b>❗ Botdan foydalanish uchun quyidagi kanallarga a'zo bo‘ling yoki so‘rov yuboring:</b>"
	var rows [][]tgbotapi.InlineKeyboardButton

	// missing map bo'sh bo'lsa logda ogohlantiramiz
	if len(missing) == 0 {
		fmt.Println("⚠️ OGOHLANTIRISH: 'missing' ro'yxati bo'sh! Foydalanuvchiga hech qanday kanal yuborilmaydi.")
	}

	for id, info := range missing {
		var inviteLink string
		var name string

		// Maxfiy yoki Ochiqligini tekshirish va logga chiqarish
		if strings.HasPrefix(info, "https://") {
			inviteLink = info
			name = "+ obuna"
			fmt.Printf("📝 Tugma qo'shildi [MAXFIY]: ID=%d | Link=%s\n", id, info)
		} else {
			cleanUsername := strings.TrimPrefix(info, "@")
			inviteLink = "https://t.me/" + cleanUsername
			name = "📢 @" + cleanUsername
			fmt.Printf("📝 Tugma qo'shildi [OCHIQ]: ID=%d | Username=%s\n", id, cleanUsername)
		}

		btn := tgbotapi.NewInlineKeyboardButtonURL(name, inviteLink)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(btn))
	}

	// Tekshirish tugmasi
	checkBtn := tgbotapi.NewInlineKeyboardButtonData("✅ Tekshirish", "check_sub")
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(checkBtn))
	fmt.Println("🔘 'Tekshirish' tugmasi qo'shildi.")

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

	_, err := bot.Send(msg)
	if err != nil {
		fmt.Printf("❌ XABAR YUBORISHDA XATO: %v\n", err)
	} else {
		fmt.Printf("✅ Xabar muvaffaqiyatli yuborildi. Jami tugmalar: %d\n", len(rows))
	}
	fmt.Println("--- 🏁 Tugmalar Yasash Tugadi ---\n")
}

func buildAnimeText(page int, perPage int) (string, int) {
	type AnimeItem struct {
		Code string
		Name string
	}

	var list []AnimeItem

	infoMutex.RLock()
	for code, name := range animeInfo {
		list = append(list, AnimeItem{Code: code, Name: name})
	}
	infoMutex.RUnlock()

	// A–Z sort
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})

	total := len(list)
	if total == 0 {
		return "❌ Anime topilmadi", 0
	}

	start := page * perPage
	if start >= total {
		start = 0
		page = 0
	}

	end := start + perPage
	if end > total {
		end = total
	}

	var b strings.Builder
	b.WriteString("🎬 *Barcha animelar (A–Z)*\n\n")

	for i, a := range list[start:end] {
		fmt.Fprintf(
			&b,
			"%d. *%s* (`%s`)\n",
			start+i+1,
			a.Name,
			strings.ToUpper(a.Code),
		)
	}

	b.WriteString(fmt.Sprintf(
		"\n📄 Sahifa: %d / %d",
		page+1,
		(total+perPage-1)/perPage,
	))

	return b.String(), total
}

func animePaginationKeyboard(page int, total int, perPage int) *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	if page > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Oldingi", fmt.Sprintf("anime_page:%d", page-1)),
		))
	}

	if (page+1)*perPage < total {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➡️ Keyingi", fmt.Sprintf("anime_page:%d", page+1)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Orqaga", "back_to_admin"),
	))

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &kb
}

func ListMaker(items []ContentItem, page int, perPage int) string {
	start := page * perPage
	end := start + perPage

	if len(items) == 0 {
		return "Mavjud qismlar yo'q."
	}

	if end > len(items) {
		end = len(items)
	}

	var list strings.Builder
	for i := start; i < end; i++ {
		// ContentItem ichida Kind maydoni borligiga ishonch hosil qiling
		kind := items[i].Kind
		if kind == "" {
			kind = "video"
		}
		list.WriteString(fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(kind)))
	}
	return list.String()
}

func NavButtons(code string, page int, totalItems int, perPage int) tgbotapi.InlineKeyboardMarkup {
	var buttons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Oldingi", fmt.Sprintf("move_del:%s:%d", code, page-1)))
	}

	totalPages := (totalItems + perPage - 1) / perPage
	if totalPages > 1 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", page+1, totalPages), "none"))
	}

	if (page+1)*perPage < totalItems {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Keyingi ➡️", fmt.Sprintf("move_del:%s:%d", code, page+1)))
	}

	return tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(buttons...))
}

func handleCallback(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery == nil {
		return
	}
	callback := update.CallbackQuery // 🔹 shu yerda e'lon qilinadi
	chatID := update.CallbackQuery.Message.Chat.ID
	userID := update.CallbackQuery.From.ID
	data := update.CallbackQuery.Data
	messageID := callback.Message.MessageID
	state := adminState[userID]
	// Telegram callback response
	defer bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

	// ------------------- FOYDALANUVCHI MANTIQI ------------------
	// CallbackQuery handler ichida
	// --- Callback Query qismi ---
	if update.CallbackQuery != nil {
		cbd := update.CallbackQuery.Data // 'callbackData' o'rniga 'cbd' deb oldik
		userID := update.CallbackQuery.From.ID
		chatID := update.CallbackQuery.Message.Chat.ID

		if strings.HasPrefix(cbd, "edit_photo:") {
			code := strings.TrimPrefix(cbd, "edit_photo:")
			animeCodeTemp[userID] = code
			adminState[userID] = "edit_anime_photo_waiting"

			bot.Send(tgbotapi.NewMessage(chatID, "🌌 Ushbu anime uchun yangi muqova suratini yuboring:"))

			// Callbackga javob berish (yuklanish belgisini olib tashlaydi)
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
		}
	}
	if update.CallbackQuery != nil {
		cbd := update.CallbackQuery.Data
		userID := update.CallbackQuery.From.ID
		chatID := update.CallbackQuery.Message.Chat.ID

		// Sahifani almashtirish (Next/Back)
		if strings.HasPrefix(cbd, "move_del:") {
			dataParts := strings.Split(strings.TrimPrefix(cbd, "move_del:"), ":")
			if len(dataParts) < 2 {
				return
			}

			animeCode := dataParts[0]
			targetPage, _ := strconv.Atoi(dataParts[1])
			limit := 30

			storageMutex.RLock()
			vids := animeStorage[animeCode]
			title := animeInfo[animeCode]
			storageMutex.RUnlock()

			newText := fmt.Sprintf("📖 *%s* (%s)\n\n🗑 **O'chirmoqchi bo'lgan qism ID raqamini yozing:**\n\n%s",
				title, strings.ToUpper(animeCode), ListMaker(vids, targetPage, limit))

			edit := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, newText)
			edit.ParseMode = "Markdown"

			markup := NavButtons(animeCode, targetPage, len(vids), limit)
			edit.ReplyMarkup = &markup

			bot.Send(edit)
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}

		// O'chirish tugmasi birinchi marta bosilganda
		if strings.HasPrefix(cbd, "delete_part:") {
			code := strings.TrimPrefix(cbd, "delete_part:")
			code = strings.ToLower(strings.TrimSpace(code))

			// Ma'lumotlarni bazadan olish (BU QATORLARNI QO'SHDIM)
			storageMutex.RLock()
			items, ok := animeStorage[code]
			name := animeInfo[code]
			storageMutex.RUnlock()

			if !ok {
				bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, "❌ Anime topilmadi"))
				return
			}

			adminMutex.Lock()
			adminState[userID] = "delete_part_id"
			animeCodeTemp[userID] = code
			adminMutex.Unlock()

			limit := 30
			msgText := fmt.Sprintf("📖 *%s* (%s)\n\n🗑 **ID raqamini yozing:**\n\n%s",
				name, strings.ToUpper(code), ListMaker(items, 0, limit))

			edit := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, msgText)
			edit.ParseMode = "Markdown"

			markup := NavButtons(code, 0, len(items), limit)
			edit.ReplyMarkup = &markup

			bot.Send(edit)
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		}
	}

	if update.Message != nil {
		userID := update.Message.From.ID
		chatID := update.Message.Chat.ID

		if update.Message.Photo != nil && adminState[userID] == "edit_anime_photo_waiting" {
			// Foto slice'dan oxirgi (eng sifatli) rasmni olamiz
			photos := update.Message.Photo
			photo := photos[len(photos)-1]

			code := animeCodeTemp[userID]

			// Rasmni saqlash
			infoMutex.Lock()
			animePhotoMap[code] = photo.FileID
			infoMutex.Unlock()

			adminState[userID] = "" // Holatni yopish
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Muqova muvaffaqiyatli yangilandi!"))
		}
	}

	if strings.HasPrefix(data, "page_") {
		// 1. Sahifa raqamini olish
		page, err := strconv.Atoi(strings.TrimPrefix(data, "page_"))
		if err != nil {
			return
		}

		chatID := update.CallbackQuery.Message.Chat.ID
		msgID := update.CallbackQuery.Message.MessageID

		// 2. playItem ni chaqirish (5 ta argument bilan)
		// Sahifa almashganda o'sha sahifadagi birinchi videoni chiqarish uchun: idx = page * 9
		playItem(bot, chatID, page*9, msgID, page)

		// 3. Callback soatini to'xtatish (Tuzatilgan qismi)
		bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
		return
	}
	if data == "check_sub" {
		// 🆕 Uchinchi argument: true (So'rov yuborgan bo'lsa o'tkazib yuborish uchun)
		isMember, _ := checkMembership(bot, userID, true)

		if isMember {
			deleteMsg := tgbotapi.NewDeleteMessage(chatID, update.CallbackQuery.Message.MessageID)
			bot.Send(deleteMsg)

			msg := tgbotapi.NewMessage(chatID, "✅ Rahmat! Botdan foydalanishingiz mumkin.")
			msg.ReplyMarkup = userMenu
			bot.Send(msg)

			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ Tasdiqlandi!"))
		} else {
			callbackConfig := tgbotapi.NewCallbackWithAlert(update.CallbackQuery.ID, "❌ Hali hamma kanalga a'zo emassiz!")
			bot.Send(callbackConfig)
		}
		return
	} // 🎵 Play tugmalari
	if strings.HasPrefix(data, "play_") {
		parts := strings.Split(data, "_")
		if len(parts) < 2 {
			return
		}

		idx, _ := strconv.Atoi(parts[1])
		page := 0
		if len(parts) >= 3 {
			page, _ = strconv.Atoi(parts[2])
		}

		messageID := update.CallbackQuery.Message.MessageID
		playItem(bot, chatID, idx, messageID, page)

		// ✅ MANA BU QISMNI O'ZGARTIRING:
		callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
		bot.Request(callbackConfig)

		return
	}

	if state == "edit_anime_photo_waiting" || state == "anime_photo" {
		if update.Message.Photo != nil {
			photos := update.Message.Photo
			photo := photos[len(photos)-1] // Eng sifatli rasm

			code := animeCodeTemp[userID]

			infoMutex.Lock()
			animePhotoMap[code] = photo.FileID
			infoMutex.Unlock()

			adminState[userID] = "" // Holatni tozalash
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Muqova muvaffaqiyatli saqlandi!"))
			return // MUHIM: Bu yerda funksiyadan chiqish kerak, pastdagi matn tekshiruviga o'tmaslik uchun
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Iltimos, faqat rasm yuboring! Matn emas."))
			return
		}
	}
	// 📄 Pagination tugmalari
	if data == "next" || data == "prev" {
		pageData, ok := userPages[chatID]
		if !ok {
			log.Printf("Pagination data not found for chat %d", chatID)
			return
		}

		totalItems := len(pageData.Items)
		totalPages := (totalItems + 9) / 10

		if data == "next" && pageData.Page+1 < totalPages {
			pageData.Page++
		} else if data == "prev" && pageData.Page > 0 {
			pageData.Page--
		}

		if update.CallbackQuery.Message != nil {
			messageID := update.CallbackQuery.Message.MessageID
			newMarkup := sendPageMenuMarkup(chatID)
			msgText := fmt.Sprintf("*%s*\nJami qism: %d\nSahifa: %d/%d",
				pageData.Name, totalItems, pageData.Page+1, totalPages)

			var req tgbotapi.Chattable
			if update.CallbackQuery.Message.Caption != "" || update.CallbackQuery.Message.Photo != nil || update.CallbackQuery.Message.Video != nil || update.CallbackQuery.Message.Document != nil {
				editCaption := tgbotapi.NewEditMessageCaption(chatID, messageID, msgText)
				editCaption.ParseMode = "Markdown"
				editCaption.ReplyMarkup = newMarkup
				req = editCaption
			} else {
				editMsg := tgbotapi.NewEditMessageText(chatID, messageID, msgText)
				editMsg.ParseMode = "Markdown"
				editMsg.ReplyMarkup = newMarkup
				req = editMsg
			}

			_, err := bot.Send(req)
			if err != nil {
				log.Printf("Xabarni tahrirlashda yakuniy xato: %v", err)
			}
		}
		return
	}
	adminMutex.Lock()
	if s, ok := adminState[userID]; ok {
		state = s
	}
	adminMutex.Unlock()
	if strings.HasPrefix(data, "vip_duration:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}

		targetID, _ := strconv.ParseInt(parts[1], 10, 64)
		durationStr := parts[2]

		var duration time.Duration
		switch durationStr {
		case "7d":
			duration = 7 * 24 * time.Hour
		case "30d":
			duration = 30 * 24 * time.Hour
		case "1y":
			duration = 365 * 24 * time.Hour
		case "60m":
			duration = 60 * 30 * 24 * time.Hour
		default:
			duration = 30 * 24 * time.Hour
		}

		vipMutex.Lock()
		if vipUsers == nil {
			vipUsers = make(map[int64]VIPUser)
		}
		vipUsers[targetID] = VIPUser{
			UserID:   targetID,
			ExpireAt: time.Now().Add(duration),
		}
		vipMutex.Unlock()

		go saveData()

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ ID: %d muvaffaqiyatli VIP qilindi! (%s)", targetID, durationStr)))

		adminMutex.Lock()
		delete(adminState, userID)
		adminMutex.Unlock()
		return
	}
	if strings.HasPrefix(data, "vip_duration:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}

		targetID, _ := strconv.ParseInt(parts[1], 10, 64)
		durationStr := parts[2]

		var duration time.Duration
		switch durationStr {
		case "7d":
			duration = 7 * 24 * time.Hour
		case "30d":
			duration = 30 * 24 * time.Hour
		case "1y":
			duration = 365 * 24 * time.Hour
		case "60m": // 60 oy
			duration = 60 * 30 * 24 * time.Hour
		default:
			duration = 30 * 24 * time.Hour
		}

		vipMutex.Lock()
		vipUsers[targetID] = VIPUser{
			UserID:   targetID,
			ExpireAt: time.Now().Add(duration),
		}
		vipMutex.Unlock()

		go saveData()

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ ID: %d muvaffaqiyatli VIP qilindi! (%s)", targetID, durationStr)))

		adminMutex.Lock()
		delete(adminState, userID)
		adminMutex.Unlock()
		return
	}
	if strings.HasPrefix(data, "vip_duration:") {
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}

		targetID, _ := strconv.ParseInt(parts[1], 10, 64)
		durationStr := parts[2]

		var duration time.Duration
		switch durationStr {
		case "7d":
			duration = 7 * 24 * time.Hour
		case "30d":
			duration = 30 * 24 * time.Hour
		case "6m":
			duration = 6 * 30 * 24 * time.Hour
		case "1y":
			duration = 365 * 24 * time.Hour
		default:
			duration = 30 * 24 * time.Hour
		}

		vipMutex.Lock()
		vipUsers[targetID] = VIPUser{
			UserID:   targetID,
			ExpireAt: time.Now().Add(duration),
		}
		vipMutex.Unlock()

		go saveData()

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ ID: %d muvaffaqiyatli VIP qilindi! (%s)", targetID, durationStr)))

		adminMutex.Lock()
		delete(adminState, userID)
		adminMutex.Unlock()
		return
	}
	if data == "check_membership" {
		// 🆕 Uchinchi argument: true
		isMember, notMemberChannels := checkMembership(bot, userID, true)

		if isMember {
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅ Obuna tekshirildi!"))
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Obuna tasdiqlandi. Kod kiritishingiz mumkin."))
		} else {
			handleMembershipCheck(bot, chatID, notMemberChannels)
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "⚠️ Avval barcha kanallarga obuna bo‘ling!"))
		}
		return
	}
	if data == "vip_list" {
		msg := tgbotapi.NewMessage(chatID, "⏳ Yuklanmoqda...")
		sent, _ := bot.Send(msg)

		sendVIPList(bot, chatID, sent.MessageID, 0)
		return
	}
	if strings.HasPrefix(data, "vip_list:") {
		page, err := strconv.Atoi(strings.TrimPrefix(data, "vip_list:"))
		if err != nil {
			return
		}

		messageID := update.CallbackQuery.Message.MessageID
		sendVIPList(bot, chatID, messageID, page)
		return
	}
	if state == "waiting_for_ad" || state == "confirm_ad" {
		// Bu yerda 'allUsers' yuborilyapti
		handleBroadcast(bot, update, adminState, broadcastCache, allUsers, &adminMutex, &requestMutex)
		return
	}
	if state == "wait_schedule_time" {
		sendTime, err := parseRelativeTime(update.Message.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Format xato! Misol: `10m` yoki `1h`"))
			return
		}

		// Yangi post ob'ektini yaratish
		scheduleMutex.Lock()
		scheduledPosts[scheduleAutoID] = &ScheduledPost{
			ID:       scheduleAutoID,
			AdminID:  userID, // Admin ID saqlab qolinadi
			SendTime: sendTime,
		}
		scheduleAutoID++
		scheduleMutex.Unlock()

		adminState[userID] = "wait_schedule_content"
		bot.Send(tgbotapi.NewMessage(chatID, "📨 Endi postni yuboring (Matn, Rasm yoki Video):"))
		return
	}
	if state == "wait_schedule_content" {
		var post *ScheduledPost

		scheduleMutex.Lock()
		for id := range scheduledPosts {
			if scheduledPosts[id].AdminID == userID && scheduledPosts[id].Content.Kind == "" {
				post = scheduledPosts[id]
				break
			}
		}
		scheduleMutex.Unlock()

		if post == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xatolik: Reja topilmadi."))
			adminState[userID] = ""
			return
		}

		// 1. Post kontentini aniqlash
		if update.Message.Photo != nil {
			photos := update.Message.Photo // '*' olib tashlandi
			post.Content = ContentItem{
				Kind:   "photo",
				FileID: photos[len(photos)-1].FileID,
				Text:   update.Message.Caption,
			}
		} else if update.Message.Video != nil {
			post.Content = ContentItem{
				Kind:   "video",
				FileID: update.Message.Video.FileID,
				Text:   update.Message.Caption,
			}
		} else if update.Message.Text != "" {
			post.Content = ContentItem{
				Kind: "text",
				Text: update.Message.Text,
			}
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Faqat matn, rasm yoki video yuboring!"))
			return
		}

		// 2. Map-ni Slice-ga o'tkazish (allUsers map[int64]bool bo'lsa)
		// post.ChatIDs []int64 bo'lishi kerak
		for uID := range allUsers {
			post.ChatIDs = append(post.ChatIDs, uID)
		}

		// 3. Rejalashtirishni ishga tushirish
		go schedulePost(bot, post)

		adminState[userID] = ""
		diff := time.Until(post.SendTime).Round(time.Second)

		// --- TUGMA QO'SHISH QISMI ---
		text := fmt.Sprintf("✅ Post rejalashtirildi!\n🕒 %s dan keyin %d kishiga yuboriladi.", diff, len(post.ChatIDs))
		msg := tgbotapi.NewMessage(chatID, text)

		// Tugma yaratish (post.ID ishlatiladi)
		cancelBtn := tgbotapi.NewInlineKeyboardButtonData("❌ Bekor qilish", fmt.Sprintf("cancel_post_%d", post.ID))
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(cancelBtn),
		)
		// ----------------------------

		bot.Send(msg)
		return
	}
	if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data

		if strings.HasPrefix(data, "cancel_post_") {
			postIDStr := strings.TrimPrefix(data, "cancel_post_")

			// Stringni INT ga o'tkazish
			postID, err := strconv.Atoi(postIDStr)
			if err != nil {
				return
			}

			scheduleMutex.Lock()
			// Endi postID (int) turida, xatolik bo'lmaydi
			delete(scheduledPosts, postID)
			scheduleMutex.Unlock()

			// To'g'ri AnswerCallbackQuery yaratish
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Post bekor qilindi!")
			bot.Request(callbackConfig)

			// Xabarni o'zgartirish
			editMsg := tgbotapi.NewEditMessageText(
				update.CallbackQuery.Message.Chat.ID,
				update.CallbackQuery.Message.MessageID,
				"🚫 Ushbu postni rejalashtirish bekor qilindi.",
			)
			bot.Send(editMsg)
		}
	}
	if admins[userID] {

		switch {
		case data == "delete_part":
			adminMutex.Lock()
			adminState[userID] = "wait_delete_code" // Birinchi kodni so'raymiz
			adminMutex.Unlock()

			bot.Send(tgbotapi.NewMessage(chatID, "🗑 Qaysi anime qismini o'chirmoqchisiz?\n\nIltimos, **anime kodini** yuboring (masalan: 57):"))
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			return
		case data == "special_commands":
			msgText := "🛠 axsus buyruqlar ro'yxati:\n\n" +
				"Quyidagi buyruqlarni botga yuborishingiz mumkin:\n\n" +
				"1️⃣ `/clear_channels` — Barcha majburiy obuna kanallarini tozalash\n" +
				"2️⃣ `/reset_stats` — Statistikani nollashtirish\n" +
				"3️⃣ `/reboot` — Bot tizimini yangilash\n\n" +
				"⚠️ *Eslatma: Bu buyruqlar faqat adminlar uchun ishlaydi!*"

			msg := tgbotapi.NewMessage(chatID, msgText)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return

		case data == "admin_schedule":
			adminState[userID] = "wait_schedule_time"
			msg := tgbotapi.NewMessage(chatID,
				"⏰ *Post yuborish vaqtini kiriting*\n\n"+
					"Masalan:\n"+
					"`10s` – 10 soniya\n"+
					"`10m` – 10 minut\n"+
					"`3h` – 3 soat\n"+
					"`2d` – 2 kun",
			)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return

		case data == "all_animes":
			page := 0
			text, total := buildAnimeText(page, 20)

			msg := tgbotapi.NewEditMessageText(
				chatID,
				update.CallbackQuery.Message.MessageID,
				text,
			)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = animePaginationKeyboard(page, total, 20)
			bot.Send(msg)
			return

		case data == "edit_anime":
			adminState[userID] = "edit_anime_code"
			bot.Send(tgbotapi.NewMessage(chatID, "✍️ Tahrirlamoqchi bo‘lgan anime kodini kiriting:"))
			return

		case data == "admin_menu":
			markup := adminManageKeyboard() // InlineKeyboardMarkup qaytaradi
			msg := tgbotapi.NewEditMessageText(
				chatID,
				update.CallbackQuery.Message.MessageID,
				"👮‍♂️ *Adminlar boshqaruvi*",
			)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = &markup // ✅ pointerga aylantirilgan
			bot.Send(msg)
			return

		case data == "list_admins":
			// 1️⃣ Adminlar sonini hisoblash
			counter := len(admins)

			// 2️⃣ Adminlar ro'yxatini yaratish
			adminList := ""
			for id := range admins {
				adminList += fmt.Sprintf("• `%d`\n", id)
			}

			// Agar hozircha admin bo‘lmasa
			if adminList == "" {
				adminList = "❌ Hozircha admin yo‘q"
			}

			// 3️⃣ Xabar tayyorlash
			message := fmt.Sprintf(
				"👥 *Jami adminlar soni:* %d\n\n"+
					"**Adminlar ro'yxati :**\n"+
					"%s\n"+
					"---",
				counter,
				adminList,
			)

			// 4️⃣ Xabarni yuborish
			msg := tgbotapi.NewMessage(chatID, message)
			msg.ParseMode = tgbotapi.ModeMarkdown // ID'larni aniq ko'rsatish uchun
			bot.Send(msg)

		case data == "show_stats":

			displayStats(bot, chatID)

		case data == "add_channel":
			adminState[userID] = "add_channel_wait"
			bot.Send(tgbotapi.NewMessage(chatID,
				"🔗 Kanal ChatID yuboring\n\n"+
					"⚠️ Eslatma: Botni kanalga ADMIN qilib qo‘shishingiz shart!",
			))

		case data == "vip_add":
			adminMutex.Lock()
			adminState[userID] = "wait_vip_add"
			adminMutex.Unlock()

			bot.Send(tgbotapi.NewMessage(chatID, "🆔 VIP qilmoqchi bo'lgan foydalanuvchi ID sini yuboring:"))
			return

		case data == "vip_del":
			adminState[userID] = "wait_vip_del"
			bot.Send(tgbotapi.NewMessage(chatID, "🆔 VIP-dan chiqarmoqchi bo'lgan foydalanuvchi ID sini yuboring:"))
			return

		case data == "user_menu":
			markup := userManageKeyboard() // Foydalanuvchi paneli
			msg := tgbotapi.NewEditMessageText(
				chatID,
				update.CallbackQuery.Message.MessageID,
				"👥 Foydalanuvchi boshqaruvi",
			)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = &markup // pointerga aylantirish
			bot.Send(msg)
			return

			// handleBroadcast ichidagi yuborish qismi

		case data == "broadcast":
			adminMutex.Lock()
			adminState[userID] = "waiting_for_ad"
			adminMutex.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, "📢 Reklama yuboring\n✍️ Matn\n🌌 Rasm\n🎥 Video\n↪️ Forward\nBitta xabar yuboring"))
			return

		case data == "broadcast_send":
			handleBroadcast(bot, update, adminState, broadcastCache, allUsers, &adminMutex, &requestMutex)
			return

		case data == "broadcast_cancel":
			adminMutex.Lock()
			delete(adminState, userID)
			delete(broadcastCache, userID)
			adminMutex.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bekor qilindi."))
			return

		case data == "back_to_admin":
			msg := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "👨‍💻 *Admin Boshqaruv Paneli:*")
			msg.ParseMode = "Markdown"
			mainMarkup := adminMenu()
			msg.ReplyMarkup = &mainMarkup // Asosiy admin menyusi
			bot.Send(msg)
			return

		case data == "upload_anime":

			adminState[userID] = "anime_name"

			bot.Send(tgbotapi.NewMessage(chatID, "🎬 anime nomini kiriting:"))

			// Agar bu logikani "delete_anime" tugmasi bosilganda ishlatmoqchi bo'lsangiz:

		case data == "delete_anime":

			// 1. Anime ro'yxatini shakllantirish
			var animeList string
			counter := 0

			// animeInfo xaritasi orqali aylanib chiqish (mutex bilan himoyalangan holda)
			infoMutex.RLock()                     // O'qish uchun bloklash
			for animeCode, _ := range animeInfo { // info o'zgaruvchisi kerak emas, shuning uchun uni '_' bilan almashtirdik
				counter++
				// ❌ info.Name olib tashlandi, faqat kod qoldi
				animeList += fmt.Sprintf("%d. Kodi: `%s`\n", counter, animeCode)
			}
			infoMutex.RUnlock() // Bloklashni yechish

			// 2. Umumiy ma'lumotni tuzish
			message := fmt.Sprintf(
				"📚 *Jami animelar soni:* %d\n\n"+
					"**anime kodlari ro'yxati:**\n"+
					"%s\n"+
					"---",
				counter,
				animeList,
			)

			// 3. Xabarni yuborish (Ro'yxat)
			msgList := tgbotapi.NewMessage(chatID, message)
			msgList.ParseMode = tgbotapi.ModeMarkdown // Kodlarni aniq ko'rsatish uchun
			bot.Send(msgList)

			// 4. Holatni saqlash va keyingi savolni berish
			adminState[userID] = "delete_anime_code"
			bot.Send(tgbotapi.NewMessage(chatID, "🗑 Yuqoridagi ro'yxatdan o‘chirmoqchi bo‘lgan anime kodini kiriting:"))

		case update.Message != nil && adminState[userID] == "add_channel_chatid":

			text := update.Message.Text

			if strings.HasPrefix(text, "https://t.me/") {

				// Havoladan username yoki joinchat linkini ajratish

				parts := strings.Split(text, "/")

				groupIdentifier := parts[len(parts)-1]

				// Bu yerda JoinChat link bo'lsa, Telegram API orqali qo‘shilish sorovi yuborish

				// Go Telegram API-da to‘g‘ridan-to‘g‘ri qo‘shish yo‘q, lekin bot o‘sha guruhga "Invite Link" orqali qo‘shiladi

				bot.Send(tgbotapi.NewMessage(chatID, "✅ Guruhga qo‘shilish so‘rovi yuborildi: "+groupIdentifier))

			} else {

				// ChatID yuborilgan holat

				chatID, err := strconv.ParseInt(text, 10, 64)

				if err != nil {

					bot.Send(tgbotapi.NewMessage(chatID, "❌ ChatID noto‘g‘ri. Qayta kiriting:"))

					return

				}

				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ ChatID %d qo‘shildi.", chatID)))

			}

			adminState[userID] = ""

		case strings.HasPrefix(data, "edit_anime_menu:"):

			code := strings.TrimPrefix(data, "edit_anime_menu:")

			infoMutex.RLock()
			name := animeInfo[code]
			infoMutex.RUnlock()

			msg := tgbotapi.NewEditMessageText(
				chatID,
				update.CallbackQuery.Message.MessageID,
				fmt.Sprintf("🎬 *%s* (%s)", name, strings.ToUpper(code)),
			)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = editMenu(code, name)
			bot.Send(msg)
			return

		case strings.HasPrefix(data, "edit_anime_menu:"):

			code := strings.TrimPrefix(data, "edit_anime_menu:")

			infoMutex.RLock()
			name := animeInfo[code]
			infoMutex.RUnlock()

			msg := tgbotapi.NewEditMessageText(
				chatID,
				update.CallbackQuery.Message.MessageID,
				fmt.Sprintf("🎬 *%s* (%s)", name, strings.ToUpper(code)),
			)
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = editMenu(code, name)
			bot.Send(msg)
			return

		case data == "remove_channel":

			// 1. Kanal ID va nomlari ro'yxatini shakllantirish
			var channelList string
			counter := 0

			// 'channels' xaritasi orqali aylanib chiqish (ChatID va Kanal Nomini o'z ichiga olgan xarita)
			for channelID, channelName := range channels {
				counter++
				// ID va nomni ro'yxatga qo'shish
				channelList += fmt.Sprintf("%d. 📢 *%s*\n   ID: `%d`\n", counter, channelName, channelID)
			}

			// 2. Umumiy ma'lumotni tuzish
			message := fmt.Sprintf(
				"🔗 *Jami ulangan kanallar soni:* %d\n\n"+
					"**Kanallar ro'yxati:**\n\n"+
					"%s\n\n"+
					"---",
				counter,
				channelList,
			)

			// 3. Xabarni yuborish
			msg := tgbotapi.NewMessage(chatID, message)
			msg.ParseMode = tgbotapi.ModeMarkdown // Nom va ID'larni aniq ko'rsatish uchun Markdown ishlatildi

			bot.Send(msg)

			// 4. Holatni saqlash va keyingi savolni berish
			adminState[userID] = "remove_channel"
			bot.Send(tgbotapi.NewMessage(chatID, "🗑 Yuqoridagi ro'yxatdan o‘chirmoqchi bo‘lgan kanal (ChatID'sini) kiriting:"))

		case data == "add_admin":

			adminState[userID] = "add_admin_id"

			bot.Send(tgbotapi.NewMessage(chatID, "➕ Yangi admin ID'sini kiriting:"))

		case data == "block_user":
			adminState[userID] = "block_user"
			bot.Send(tgbotapi.NewMessage(chatID, "🚫 Bloklanadigan foydalanuvchi ID'sini kiriting:"))

		case data == "remove_admin":

			// 1. Admin IDlar ro'yxatini shakllantirish
			var adminList string
			counter := 0

			// 'admins' xaritasi orqali aylanib chiqish
			for adminID, _ := range admins {
				counter++
				// IDni ro'yxatga qo'shish
				adminList += fmt.Sprintf("%d. ID: `%d`\n", counter, adminID)
				// Agar asosiy admin IDsi bo'lsa, buni ham belgilash mumkin
				// if adminID == MAIN_ADMIN_ID {
				//     adminList += " (Asosiy Admin)\n"
				// } else {
				//     adminList += "\n"
				// }
			}

			// 2. Umumiy ma'lumotni tuzish
			message := fmt.Sprintf(
				"👥 *Jami adminlar soni:* %d\n\n"+
					"**Adminlar ro'yxati :**\n"+
					"%s\n"+
					"---",
				counter,
				adminList,
			)

			// 3. Xabarni yuborish
			msg := tgbotapi.NewMessage(chatID, message)
			msg.ParseMode = tgbotapi.ModeMarkdown // ID'larni aniq ko'rsatish uchun Markdown ishlatildi

			bot.Send(msg)

			// 4. Holatni saqlash va keyingi savolni berish
			adminState[userID] = "remove_admin_id"
			bot.Send(tgbotapi.NewMessage(chatID, "🗑 Yuqoridagi ro'yxatdan o‘chirmoqchi bo‘lgan admin ID'sini kiriting:"))

		case data == "unblock_user":

			adminState[userID] = "unblock_user"

			bot.Send(tgbotapi.NewMessage(chatID, "♻️ Blokdan chiqariladigan foydalanuvchi ID'sini kiriting:"))

		case data == "blocked_list":

			displayBlockedUsers(bot, chatID)

		case data == "new_anime_upload": // 👈 YANGI ANIME QO'SHISH UCHUN (EditMenu ichidagi tugma)

			adminState[userID] = "anime_name"

			bot.Send(tgbotapi.NewMessage(chatID, "🎬 Yangi anime nomini kiriting:"))

			// ------------------- TAHRIRLASH BUYRUQLARI (strings.HasPrefix) -------------------

			// Nomni o'zgartirishni boshlash

		case strings.HasPrefix(data, "delete_part:"):

			code := strings.TrimPrefix(data, "delete_part:")

			animeCodeTemp[userID] = code

			adminState[userID] = "delete_part_id"

			storageMutex.RLock()

			items := animeStorage[code]

			storageMutex.RUnlock()

			name := animeInfo[code]

			// 🔥 MUHIM: partList ni boshlang'ich qiymat bilan e'lon qilish

			partList := ""

			// Qismlarni ro'yxatlash

			if len(items) > 0 {

				for i, item := range items {

					partList += fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(item.Kind))

				}

			} else {

				partList = "Mavjud qismlar yo'q."

			}

			msgText := fmt.Sprintf("🗑 **%s** (%s) uchun o‘chirmoqchi bo‘lgan **qism ID raqamini kiriting:\n\n-- Qismlar Ro'yxati --\n%s", name, strings.ToUpper(code), partList)

			msg := tgbotapi.NewMessage(chatID, msgText)

			msg.ParseMode = "Markdown"

			bot.Send(msg)

			// Qismni ID bo'yicha o'chirishni boshlash

		case strings.HasPrefix(data, "delete_part:"):

			code := strings.TrimPrefix(data, "delete_part:")

			animeCodeTemp[userID] = code

			adminState[userID] = "delete_part_id"

			storageMutex.RLock()

			items := animeStorage[code]

			storageMutex.RUnlock()

			name := animeInfo[code]

			// Qismlarni ro'yxatlash

			partList := ""

			if len(items) > 0 {

				for i, item := range items {

					partList += fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(item.Kind))

				}

			} else {

				partList = "Mavjud qismlar yo'q."

			}

			msgText := fmt.Sprintf("🗑 **%s** (%s) uchun o‘chirmoqchi bo‘lgan **qism ID raqamini** (1, 2, 3...) kiriting:\n\n-- Qismlar Ro'yxati --\n%s", name, strings.ToUpper(code), partList)

			msg := tgbotapi.NewMessage(chatID, msgText)

			msg.ParseMode = "Markdown"

			bot.Send(msg)

			// Anime to'liq o'chirishni tasdiqlash

		case strings.HasPrefix(data, "edit_name:"):

			code := strings.TrimPrefix(data, "edit_name:")

			animeCodeTemp[userID] = code

			adminState[userID] = "edit_new_name"

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✍️ '%s' uchun yangi nom kiriting:", strings.ToUpper(code))))

			// Kodni o'zgartirishni boshlash

		case strings.HasPrefix(data, "edit_code:"):

			code := strings.TrimPrefix(data, "edit_code:")

			animeCodeTemp[userID] = code

			adminState[userID] = "edit_new_code"

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🆔 '%s' uchun yangi kod kiriting:", strings.ToUpper(code))))

			// handleCallback funksiyasi ichidagi switch { ... }

			// handleCallback funksiyasi ichidagi switch { ... }

		case strings.HasPrefix(data, "edit_content:"):

			code := strings.TrimPrefix(data, "edit_content:")

			// Anime nomi animeInfo mapidan olinadi

			infoMutex.RLock()

			name := animeInfo[code]

			infoMutex.RUnlock()

			// 🔥 Hozirgi qismlar sonini hisoblash

			storageMutex.RLock()

			currentCount := len(animeStorage[code])

			storageMutex.RUnlock()

			// Admin holatlarini saqlash

			animeCodeTemp[userID] = code

			animeNameTemp[userID] = name

			adminState[userID] = "anime_videos" // Kontent yuklash rejimiga o'tish

			// 🔥 YANGI XABAR MATNI

			msgText := fmt.Sprintf(

				"🎬 **Nom:** **%s**\n🆔 **Kod:** (%s)\n\n**Hozirgi qismlar:** %d ta.\n**Yangi kontent:** %d-qismdan boshlab qo‘shiladi.\n\nEndi qoʻshmoqchi boʻlgan **video, fayl yoki photeni** yuboring. Tugatgach **/ok** deb yozing.",

				name,

				strings.ToUpper(code), // Kodni katta harflarda ko'rsatamiz

				currentCount,

				currentCount+1, // Yangi qism tartib raqami

			)

			msg := tgbotapi.NewMessage(chatID, msgText)

			msg.ParseMode = "Markdown"

			bot.Send(msg)

			return

		case strings.HasPrefix(data, "delete_part_page:"):
			parts := strings.Split(data, ":")
			if len(parts) < 3 {
				return
			}
			code := parts[1]
			page, _ := strconv.Atoi(parts[2])

			storageMutex.RLock()
			items := animeStorage[code]
			name := animeInfo[code]
			storageMutex.RUnlock()

			partList := buildPartList(items, page, 15)
			keyboard := buildPaginationKeyboard(code, page, len(items), 15)

			msg := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID,
				fmt.Sprintf("🗑 **%s** (%s)\nID ni kiriting:\n\n%s", name, strings.ToUpper(code), partList))
			msg.ParseMode = "Markdown"
			msg.ReplyMarkup = &keyboard
			bot.Send(msg)
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

			// 1. O'chirishni tasdiqlash so'rash (Bir marta bo'lishi shart!)
		case strings.HasPrefix(data, "delete_anime_confirm:"):
			code := strings.TrimPrefix(data, "delete_anime_confirm:")

			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("✅ Ha, o'chirilsin", "delete_anime_final:"+code),
					tgbotapi.NewInlineKeyboardButtonData("❌ Yo'q, bekor qilish", "delete_anime_cancel"),
				),
			)

			// Callback soatini yo'qotish
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❗ **%s** animeni o'chirmoqchimisiz?", animeInfo[code]))
			msg.ReplyMarkup = keyboard
			bot.Send(msg)
			return

			// 2. HA, o'chirilsin deb bosilganda
		case strings.HasPrefix(data, "delete_anime_final:"):
			code := strings.TrimPrefix(data, "delete_anime_final:")

			// Ma'lumotlarni o'chirish
			infoMutex.Lock()
			delete(animeInfo, code)
			delete(animePhotos, code)
			delete(animePhotoMap, code)
			infoMutex.Unlock()

			storageMutex.Lock()
			delete(animeStorage, code)
			storageMutex.Unlock()

			go saveData()
			go saveAnimePhotos()

			// Tugmalarni yo'qotib, xabarni o'zgartirish
			edit := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "✅ Anime bazadan butunlay o'chirildi!")
			bot.Send(edit)

			// Soatni yo'qotish
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "O'chirildi!"))
			return

			// 3. YO'Q, bekor qilish deb bosilganda
		case data == "delete_anime_cancel":
			edit := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "❌ O'chirish bekor qilindi.")
			bot.Send(edit)

			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "Bekor qilindi"))
			return

		case data == "delete_anime_cancel":
			edit := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "❌ O'chirish bekor qilindi.")
			bot.Send(edit)
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "Bekor qilindi"))

		default:
			if update.CallbackQuery != nil || adminState[userID] != "" {
				return
			}

			bot.Send(tgbotapi.NewMessage(chatID, "❓ Noma'lum buyruq. Iltimos, menu'dan tanlang."))

		}

		return

	}
	if update.CallbackQuery != nil {
		callback := update.CallbackQuery
		data := callback.Data
		chatID := callback.Message.Chat.ID
		messageID := callback.Message.MessageID

		if strings.HasPrefix(data, "delete_part_page:") {
			parts := strings.Split(data, ":")
			code := parts[1]
			page, _ := strconv.Atoi(parts[2])

			items := animeStorage[code] // []ContentItem
			name := animeInfo[code]

			partList := buildPartList(items, page, 15)

			text := fmt.Sprintf(
				"🗑 %s (%s) uchun o‘chirmoqchi bo‘lgan qism ID raqamini kiriting:\n\n-- Qismlar Ro'yxati --\n%s",
				name,
				strings.ToUpper(code),
				partList,
			)

			keyboard := buildPaginationKeyboard(code, page, len(items), 15)

			edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
			edit.ParseMode = "Markdown"
			edit.ReplyMarkup = &keyboard

			_, err := bot.Send(edit)
			if err != nil {
				log.Println("EditMessageText error:", err)
			}
		}
	}
	if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data
		chatID := update.CallbackQuery.Message.Chat.ID
		messageID := update.CallbackQuery.Message.MessageID

		// REORDER PAGINATION QISMI
		if strings.HasPrefix(data, "reorder_page:") {
			parts := strings.Split(data, ":")
			code := parts[1]
			page, _ := strconv.Atoi(parts[2])

			storageMutex.RLock()
			items := animeStorage[code]
			name := animeInfo[code]
			storageMutex.RUnlock()

			partList := buildPartList(items, page, 15)
			text := fmt.Sprintf("🔢 **%s** qismlarini tartiblash\n\n-- Qismlar Ro'yxati --\n%s", name, partList)

			// Muhim: Tugmalarni qayta yasash (Keyingi/Oldingi)
			keyboard := buildReorderPaginationKeyboard(code, page, len(items), 15)

			// Xabarni tahrirlash (yangi xabar yubormaslik kerak)
			edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
			edit.ParseMode = "Markdown"
			edit.ReplyMarkup = &keyboard

			bot.Send(edit)

			// Telegramga javob qaytarish (soat belgisi ketishi uchun)
			bot.Send(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
		}
	}
	if strings.HasPrefix(data, "reorder_page:") {
		parts := strings.Split(data, ":")
		code := parts[1]
		page, _ := strconv.Atoi(parts[2])

		storageMutex.RLock()
		items := animeStorage[code]
		name := animeInfo[code]
		storageMutex.RUnlock()

		partList := buildPartList(items, page, 15)

		text := fmt.Sprintf(
			"🔢 **%s** (%s) qismlarini tartiblash\n\n-- Qismlar Ro'yxati --\n%s",
			name, strings.ToUpper(code), partList,
		)

		keyboard := buildReorderPaginationKeyboard(code, page, len(items), 15)

		edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
		edit.ParseMode = "Markdown"
		edit.ReplyMarkup = &keyboard
		bot.Send(edit)
	}
	if strings.HasPrefix(data, "reorderAnime:") {
		code := strings.TrimPrefix(data, "reorderAnime:")

		storageMutex.RLock()
		list := animeStorage[code]
		storageMutex.RUnlock()

		if len(list) == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Qismlar topilmadi."))
			return
		}

		var sb strings.Builder
		sb.WriteString("📋 *Qismlar ro'yxati:\n\n")

		for i, item := range list {
			sb.WriteString(fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(item.Kind)))
		}

		sb.WriteString("\n📝 Yangi tartibni yuboring (masalan: 1,2,3,5,4)")

		msg := tgbotapi.NewMessage(chatID, sb.String())
		msg.ParseMode = "Markdown"
		bot.Send(msg)

		adminState[userID] = "anime_reorder:" + code
		return
	}

}

func buildReorderPaginationKeyboard(code string, page int, total int, perPage int) tgbotapi.InlineKeyboardMarkup {
	var buttons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Oldingi", fmt.Sprintf("reorder_page:%s:%d", code, page-1)))
	}
	if (page+1)*perPage < total {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("➡️ Keyingi", fmt.Sprintf("reorder_page:%s:%d", code, page+1)))
	}

	row := tgbotapi.NewInlineKeyboardRow(buttons...)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

func buildPartList(items []ContentItem, page int, perPage int) string {
	start := page * perPage
	end := start + perPage

	if start >= len(items) {
		return "Qismlar topilmadi."
	}
	if end > len(items) {
		end = len(items)
	}

	text := ""
	for i := start; i < end; i++ {
		text += fmt.Sprintf("ID: %d | Turi: %s\n", i+1, items[i].Kind)
	}
	return text
}

func buildPaginationKeyboard(code string, page int, total int, perPage int) tgbotapi.InlineKeyboardMarkup {
	var buttons []tgbotapi.InlineKeyboardButton

	if page > 0 {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("⬅️ Oldingi", fmt.Sprintf("delete_part_page:%s:%d", code, page-1)))
	}
	if (page+1)*perPage < total {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("➡️ Keyingi", fmt.Sprintf("delete_part_page:%s:%d", code, page+1)))
	}

	row := tgbotapi.NewInlineKeyboardRow(buttons...)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

func schedulePost(bot *tgbotapi.BotAPI, post *ScheduledPost) {
	delay := time.Until(post.SendTime)
	if delay > 0 {
		time.Sleep(delay)
	}

	// ❗ BEKOR QILINGANMI — TEKSHIRAMIZ
	scheduleMutex.Lock()
	_, exists := scheduledPosts[post.ID]
	scheduleMutex.Unlock()

	if !exists {
		// Post bekor qilingan, hech narsa qilmaymiz
		return
	}

	sentCount := 0
	for _, chatID := range post.ChatIDs {
		var msg tgbotapi.Chattable

		switch post.Content.Kind {
		case "photo":
			p := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(post.Content.FileID))
			p.Caption = post.Content.Text
			msg = p

		case "video":
			v := tgbotapi.NewVideo(chatID, tgbotapi.FileID(post.Content.FileID))
			v.Caption = post.Content.Text
			msg = v

		case "text":
			msg = tgbotapi.NewMessage(chatID, post.Content.Text)
		}

		if _, err := bot.Send(msg); err == nil {
			sentCount++
		}
		time.Sleep(33 * time.Millisecond)
	}

	// ❗ YUBORISHDAN KEYIN HAM YANA TEKSHIRISH (ixtiyoriy, lekin yaxshi)
	scheduleMutex.Lock()
	_, exists = scheduledPosts[post.ID]
	if exists {
		delete(scheduledPosts, post.ID) // Tozalab qo'yamiz
	}
	scheduleMutex.Unlock()

	if !exists {
		return
	}

	// Admin-ga hisobot
	report := fmt.Sprintf(
		"✅ Rejalashtirilgan post tugatildi!\n\n📊 Jami: %d kishiga yuborildi.",
		sentCount,
	)
	bot.Send(tgbotapi.NewMessage(post.AdminID, report))
}

func parseRelativeTime(input string) (time.Time, error) {
	now := time.Now()
	input = strings.TrimSpace(strings.ToLower(input))
	if len(input) < 2 {
		return time.Time{}, fmt.Errorf("invalid input")
	}

	unit := input[len(input)-1:]   // oxirgi harfni oladi: s, m, h, d
	numStr := input[:len(input)-1] // raqam qismini oladi

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return time.Time{}, err
	}

	var dur time.Duration
	switch unit {
	case "s":
		dur = time.Duration(num) * time.Second
	case "m":
		dur = time.Duration(num) * time.Minute
	case "h":
		dur = time.Duration(num) * time.Hour
	case "d":
		dur = time.Duration(num) * 24 * time.Hour
	default:
		return time.Time{}, fmt.Errorf("unknown unit")
	}

	return now.Add(dur), nil
}

func handleSchedule(bot *tgbotapi.BotAPI, update tgbotapi.Update, state string) {
	var userID int64
	var chatID int64

	// Xavfsiz identifikatsiya: Update qayerdan kelganini tekshiramiz
	if update.Message != nil {
		userID = update.Message.From.ID
		chatID = update.Message.Chat.ID
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		chatID = update.CallbackQuery.Message.Chat.ID
	} else {
		return // Agar ikkalasi ham bo'lmasa, funksiyadan chiqamiz
	}

	if state == "wait_schedule_time" {
		if update.Message == nil {
			return
		} // Vaqtni faqat matnli xabardan olamiz

		sendTime, err := parseRelativeTime(update.Message.Text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Format xato! Misol: `10m` yoki `1h`"))
			return
		}

		scheduleMutex.Lock()
		// ScheduledPost mapini tekshirish va yaratish
		if scheduledPosts == nil {
			scheduledPosts = make(map[int]*ScheduledPost)
		}

		scheduledPosts[scheduleAutoID] = &ScheduledPost{
			ID:       scheduleAutoID,
			AdminID:  userID,
			SendTime: sendTime,
			ChatIDs:  make([]int64, 0), // Slice-ni inicializatsiya qilish
		}
		scheduleAutoID++
		scheduleMutex.Unlock()

		adminMutex.Lock()
		adminState[userID] = "wait_schedule_content"
		adminMutex.Unlock()

		bot.Send(tgbotapi.NewMessage(chatID, "📨 Endi postni yuboring (Matn, Rasm yoki Video):"))
		return
	}
	if state == "wait_schedule_content" {
		if update.Message == nil {
			return
		}

		// 🔽 AYNAN SHU YERGA QO‘YILADI
		var post *ScheduledPost

		scheduleMutex.Lock()
		for _, p := range scheduledPosts {
			if p.AdminID == userID && p.Content.Kind == "" {
				post = p
				break
			}
		}
		scheduleMutex.Unlock()
		// 🔼 SHU YERDA TUGAYDI

		// Agar rejalashtirilgan post topilmasa
		if post == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Reja topilmadi. Qayta urinib ko'ring."))
			adminMutex.Lock()
			adminState[userID] = ""
			adminMutex.Unlock()
			return
		}

		// --- 1. Kontentni yuklash ---
		if update.Message.Photo != nil {
			post.Content = ContentItem{
				Kind:   "photo",
				FileID: update.Message.Photo[len(update.Message.Photo)-1].FileID,
				Text:   update.Message.Caption,
			}
		} else if update.Message.Video != nil {
			post.Content = ContentItem{
				Kind:   "video",
				FileID: update.Message.Video.FileID,
				Text:   update.Message.Caption,
			}
		} else if update.Message.Text != "" {
			post.Content = ContentItem{
				Kind: "text",
				Text: update.Message.Text,
			}
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Faqat rasm, video yoki matn yuboring."))
			return
		}

		// --- 2. Foydalanuvchilarni yig‘ish ---
		statsMutex.Lock()
		post.ChatIDs = make([]int64, 0, len(userJoined))
		for uID := range userJoined {
			post.ChatIDs = append(post.ChatIDs, uID)
		}
		statsMutex.Unlock()

		// --- 3. Postni ishga tushirish ---
		go schedulePost(bot, post)

		// Holatni tozalash
		adminMutex.Lock()
		adminState[userID] = ""
		adminMutex.Unlock()

		// --- 4. Tasdiqlash xabari ---
		diff := time.Until(post.SendTime).Round(time.Second)
		msgText := fmt.Sprintf(
			"✅ Post rejalashtirildi!\n🕒 %s dan keyin %d kishiga yuboriladi.",
			diff.String(),
			len(post.ChatIDs),
		)

		msg := tgbotapi.NewMessage(chatID, msgText)
		cancelBtn := tgbotapi.NewInlineKeyboardButtonData(
			"❌ Bekor qilish",
			fmt.Sprintf("cancel_post_%d", post.ID),
		)
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(cancelBtn),
		)

		bot.Send(msg)
		return
	}

	// --- CallbackQuery bilan ishlash ---
	if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data

		if strings.HasPrefix(data, "cancel_post_") {
			idStr := strings.TrimPrefix(data, "cancel_post_")
			postID, err := strconv.Atoi(idStr)
			if err != nil {
				return
			}

			scheduleMutex.Lock()
			delete(scheduledPosts, postID)
			scheduleMutex.Unlock()

			// Xabarni o‘zgartirish
			editMsg := tgbotapi.NewEditMessageText(
				update.CallbackQuery.Message.Chat.ID,
				update.CallbackQuery.Message.MessageID,
				"🚫 Ushbu postni rejalashtirish bekor qilindi.",
			)
			bot.Send(editMsg)

			// Telegramga callback javob (loading yo‘qoladi)
			bot.Request(
				tgbotapi.NewCallback(update.CallbackQuery.ID, "Bekor qilindi"),
			)

			return // ❗❗❗ ENG MUHIM QATOR
		}
	}

}

func handleBroadcast(bot *tgbotapi.BotAPI, update tgbotapi.Update, adminState map[int64]string, broadcastCache map[int64]*tgbotapi.Message, targetUsers interface{}, adminMutex *sync.Mutex, requestMutex *sync.RWMutex) {
	var userID int64
	var chatID int64

	// ------------------ IDENTIFY ------------------
	if update.Message != nil {
		userID = update.Message.From.ID
		chatID = update.Message.Chat.ID
	} else if update.CallbackQuery != nil {
		userID = update.CallbackQuery.From.ID
		chatID = update.CallbackQuery.Message.Chat.ID
	} else {
		return
	}

	// ------------------ CALLBACK ------------------
	if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data

		switch data {
		// Callback qismini shunday o'zgartiring:
		// Global o'zgaruvchilar (main yoki teparoqda e'lon qiling)

		// handleBroadcast ichidagi Callback qismini quyidagicha o'zgartiring:
		case "broadcast_send":
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))

			broadcastMutex.Lock()
			if isBroadcasting {
				broadcastMutex.Unlock()
				bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Hozirda reklama yuborish jarayoni ketmoqda. Tugashini kuting yoki to'xtating."))
				return
			}

			adminMutex.Lock()
			msg, ok := broadcastCache[userID]
			adminMutex.Unlock()

			if !ok || msg == nil {
				broadcastMutex.Unlock()
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Xabar topilmadi."))
				return
			}

			// Jarayonni boshlash
			isBroadcasting = true
			ctx, cancel := context.WithCancel(context.Background())
			broadcastCancel = cancel
			broadcastMutex.Unlock()

			go func(targetMsg tgbotapi.Message, targetChatID int64, localCtx context.Context) {
				// 1. Jarayon yakunlanganda flagni albatta ochish
				defer func() {
					broadcastMutex.Lock()
					isBroadcasting = false
					broadcastMutex.Unlock()
					log.Println("Reklama jarayoni yakunlandi (isBroadcasting = false)")
				}()

				usersMutex.RLock()
				// Userlarni nusxalash (RLock'ni tezroq bo'shatish uchun)
				userList := make([]int64, 0, len(users))
				for uid := range users {
					userList = append(userList, uid)
				}
				usersMutex.RUnlock()

				stopKeyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🛑 Majburiy to'xtatish", "broadcast_stop"),
					),
				)

				statusMsg := tgbotapi.NewMessage(targetChatID, "📢 Reklama yuborish boshlandi...")
				statusMsg.ReplyMarkup = stopKeyboard
				bot.Send(statusMsg)

				sent := 0
				for i, uid := range userList {
					select {
					case <-localCtx.Done(): // Context bekor qilinganda (Stop tugmasi)
						bot.Send(tgbotapi.NewMessage(targetChatID, fmt.Sprintf("🛑 Jarayon majburiy to'xtatildi. %d kishiga yuborildi.", sent)))
						return // Funktsiyadan chiqish (defer ishlaydi)
					default:
						copyMsg := tgbotapi.NewCopyMessage(uid, targetMsg.Chat.ID, targetMsg.MessageID)
						_, err := bot.Send(copyMsg)
						if err == nil {
							sent++
						}

						// Telegram limitlarini hisobga olish (Anti-flood)
						if (i+1)%30 == 0 {
							time.Sleep(1 * time.Second)
						} else {
							time.Sleep(35 * time.Millisecond)
						}
					}
				}
				bot.Send(tgbotapi.NewMessage(targetChatID, fmt.Sprintf("✅ Tugatildi. Jami: %d", sent)))
			}(*msg, chatID, ctx)
			return

		case "broadcast_stop":
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "To'xtatilmoqda..."))
			broadcastMutex.Lock()
			if broadcastCancel != nil {
				broadcastCancel() // Goroutineni to'xtatadi
			}
			broadcastMutex.Unlock()
			return

		case "broadcast_cancel":
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, ""))
			broadcastMutex.Lock()
			if isBroadcasting {
				broadcastMutex.Unlock()
				bot.Send(tgbotapi.NewMessage(chatID, "🚫 Jarayon ketayotgan vaqtda bekor qilib bo'lmaydi. Avval to'xtating!"))
				return
			}
			broadcastMutex.Unlock()

			adminMutex.Lock()
			delete(adminState, userID)
			delete(broadcastCache, userID)
			adminMutex.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Kesh tozalandi."))
			return
		}
	}

	// ------------------ MESSAGE ------------------
	adminMutex.Lock()
	state := adminState[userID]
	adminMutex.Unlock()

	if update.Message != nil && state == "waiting_for_ad" {
		// Xabarni saqlash
		adminMutex.Lock()
		broadcastCache[userID] = update.Message
		adminState[userID] = "confirm_ad"
		adminMutex.Unlock()

		// Preview yuborish
		preview := tgbotapi.NewCopyMessage(chatID, update.Message.Chat.ID, update.Message.MessageID)
		bot.Send(preview)

		// Inline confirm tugmalar
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Yuborish", "broadcast_send"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Bekor qilish", "broadcast_cancel"),
			),
		)

		confirm := tgbotapi.NewMessage(chatID, "⬆️ Reklama tayyor. Yuboraymi?")
		confirm.ReplyMarkup = keyboard
		bot.Send(confirm)
	}
}

func displayStats(bot *tgbotapi.BotAPI, chatID int64) {

	statsMutex.Lock()
	defer statsMutex.Unlock()

	var sb strings.Builder

	sb.WriteString("✨ <b>UMUMIY BOT STATISTIKASI</b> ✨\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// 1️⃣ BOT FAOLIYATI
	sb.WriteString("📊 <b>BOT FAOLIYATI</b>\n")
	sb.WriteString(fmt.Sprintf("👋 /start buyurganlar: <b>%d</b>\n", startCount))
	sb.WriteString(fmt.Sprintf("👥 Jami foydalanuvchilar: <b>%d</b>\n", len(users)))
	sb.WriteString(fmt.Sprintf("🚫 Bloklanganlar: <b>%d</b>\n\n", len(blockedUsers)))

	// 3️⃣ ENG MASHHUR 5 ANIME
	sb.WriteString("🏆 <b>ENG MASHHUR 5 anime</b>\n")

	type kv struct {
		Code  string
		Count int
	}

	var arr []kv
	for code, count := range searchStats {
		arr = append(arr, kv{code, count})
	}

	sort.Slice(arr, func(i, j int) bool {
		return arr[i].Count > arr[j].Count
	})

	if len(arr) == 0 {
		sb.WriteString("— Statistika hali yetarli emas\n\n")
	} else {
		if len(arr) > 5 {
			arr = arr[:5]
		}
		for i, v := range arr {
			name := animeInfo[v.Code]
			if name == "" {
				name = "Nomaʼlum"
			}
			sb.WriteString(fmt.Sprintf("%d. <b>%s</b> (<code>%s</code>) — %d ta\n",
				i+1, name, strings.ToUpper(v.Code), v.Count))
		}
		sb.WriteString("\n")
	}

	// 4️⃣ KANALLAR
	sb.WriteString("🔗 <b>KANAL OBUNALARI</b>\n")
	if len(channels) == 0 {
		sb.WriteString("— Kanal ulanmagan\n\n")
	} else {
		for _, ch := range channels {
			sb.WriteString(fmt.Sprintf("✅ @%s\n", ch))
		}
		sb.WriteString("\n")
	}

	// 5️⃣ FOYDALANUVCHI STATISTIKASI
	active, inactive,
		todayNew, weekNew, monthNew,
		todayActive, weekActive, monthActive := calculateUserStats()

	sb.WriteString("📊 <b>FOYDALANUVCHI STATISTIKASI</b>\n")
	sb.WriteString(fmt.Sprintf("🟢 Faol: <b>%d</b>\n", active))
	sb.WriteString(fmt.Sprintf("🚫 Nofaol: <b>%d</b>\n\n", inactive))

	sb.WriteString("🆕 <b>OBUNACHILAR</b>\n")
	sb.WriteString(fmt.Sprintf("📅 Bugungi: <b>%d</b>\n", todayNew))
	sb.WriteString(fmt.Sprintf("🗓 7 kunlik: <b>%d</b>\n", weekNew))
	sb.WriteString(fmt.Sprintf("🗓 30 kunlik: <b>%d</b>\n\n", monthNew))

	sb.WriteString("🔥 <b>AKTIVLIK</b>\n")
	sb.WriteString(fmt.Sprintf("⚡ Bugungi: <b>%d</b>\n", todayActive))
	sb.WriteString(fmt.Sprintf("📈 7 kunlik: <b>%d</b>\n", weekActive))
	sb.WriteString(fmt.Sprintf("📊 30 kunlik: <b>%d</b>\n\n", monthActive))

	sb.WriteString("ℹ️ <i>Maʼlumotlar server vaqti bilan yangilandi</i>")

	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ParseMode = tgbotapi.ModeHTML

	_, err := bot.Send(msg)
	if err != nil {
		log.Println("Statistika yuborishda xato:", err)
	}
}

func displayBlockedUsers(bot *tgbotapi.BotAPI, chatID int64) {
	// Implement blocked users list display logic here
	var blockedList []string
	for id := range blockedUsers {
		blockedList = append(blockedList, strconv.FormatInt(id, 10))
	}
	bot.Send(tgbotapi.NewMessage(chatID, "📵 Bloklangan foydalanuvchilar:\n"+strings.Join(blockedList, " \n ")))
}

func updateUserActivity(userID int64) {
	now := time.Now()

	statsMutex.Lock()
	defer statsMutex.Unlock()

	// 🔥 foydalanuvchini ro‘yxatga qo‘shish
	users[userID] = true

	// oxirgi aktivlik
	userLastActive[userID] = now

	// birinchi kirish vaqti
	if _, ok := userJoinedAt[userID]; !ok {
		userJoinedAt[userID] = now
	}
}

func sendPageMenuMarkup(chatID int64) *tgbotapi.InlineKeyboardMarkup {
	data := userPages[chatID]
	if data == nil {
		return nil
	}
	start := data.Page * 10
	end := start + 10
	if end > len(data.Items) {
		end = len(data.Items)
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	var currentRow []tgbotapi.InlineKeyboardButton
	// Qism tugmalari 10 tadan ko'rsatiladi
	for i := start; i < end; i++ {
		label := fmt.Sprintf("%d", i+1)
		cb := fmt.Sprintf("play_%d", i)
		currentRow = append(currentRow, tgbotapi.NewInlineKeyboardButtonData(label, cb))
		// Har bi
		if len(currentRow) == 3 || i == end-1 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}
	nav := []tgbotapi.InlineKeyboardButton{}
	totalPages := (len(data.Items) + 9) / 10 // Jami sahifalar soni
	// Navigatsiya tugmalari
	// ⬅️ Olingi (<) tugmasi
	if data.Page > 0 {
		// Matnni faqat "<" ga o'zgartirdik
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("<", "prev"))
	}
	// Sahifa raqamini ko'rsatuvchi tugmani Olib tashladik (Sizning talabingizga ko'ra)
	// nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%d/%d", data.Page+1, totalPages), "page_count"))
	// ➡️ Keyingi (>) tugmasi
	if data.Page+1 < totalPages { // Agar joriy sahifa oxirgisidan oldin bo'lsa

		// Matnni faqat ">" ga o'zgartirdik

		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(">", "next"))
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	markup := tgbotapi.NewInlineKeyboardMarkup(rows...)
	return &markup
}

func calculateUserStats() (active int, inactive int, todayNew int, weekNew int, monthNew int, todayActive int, weekActive int, monthActive int) {
	now := time.Now()

	for userID := range users {

		// 🆕 Obuna statistikasi
		if joinTime, ok := userJoinedAt[userID]; ok {
			if joinTime.After(now.Add(-24 * time.Hour)) {
				todayNew++
			}
			if joinTime.After(now.Add(-7 * 24 * time.Hour)) {
				weekNew++
			}
			if joinTime.After(now.Add(-30 * 24 * time.Hour)) {
				monthNew++
			}
		}

		// 🔥 Aktivlik statistikasi
		if last, ok := userLastActive[userID]; ok {
			if last.After(now.Add(-24 * time.Hour)) {
				todayActive++
				weekActive++
				monthActive++
				active++
			} else if last.After(now.Add(-7 * 24 * time.Hour)) {
				weekActive++
				monthActive++
				active++
			} else if last.After(now.Add(-30 * 24 * time.Hour)) {
				monthActive++
				active++
			} else {
				inactive++
			}
		} else {
			inactive++
		}
	}

	return
}

func saveData() {
	// ========================
	// 1️⃣ Anime Storage (kontent)
	// ========================
	storageMutex.RLock()
	storageData, err := json.MarshalIndent(animeStorage, "", "  ")
	storageMutex.RUnlock()
	if err != nil {
		log.Printf("❌ anime storage JSON xatosi: %v", err)
	} else {
		if err := os.WriteFile(ANIME_STORAGE_FILE, storageData, 0644); err != nil {
			log.Printf("❌ anime storage faylga yozishda xato: %v", err)
		}
	}

	// ========================
	// 2️⃣ Anime Info & Photos (YANGILANDI)
	// ========================
	infoMutex.RLock()
	// Ismlarni saqlash
	infoData, err1 := json.MarshalIndent(animeInfo, "", "  ")
	// 📸 Rasmlarni saqlash (Yangi qo'shildi)
	// anime_photos emas, animePhotos bo'lishi kerak!
	photoData, err2 := json.MarshalIndent(animePhotos, "", "  ")

	if err1 != nil {
		log.Printf("❌ anime info JSON xatosi: %v", err1)
	} else {
		_ = os.WriteFile(ANIME_INFO_FILE, infoData, 0644)
	}

	if err2 != nil {
		log.Printf("❌ anime photos JSON xatosi: %v", err2)
	} else {
		// ANIME_PHOTOS_FILE konstantasini tepada e'lon qilishni unutmang
		_ = os.WriteFile(ANIME_PHOTOS_FILE, photoData, 0644)
	}

	// ========================
	// 3️⃣ Adminlar, Kanallar, Foydalanuvchilar
	// ========================
	config := AdminConfig{
		Admins:   adminIDs,
		Channels: channels,
		AllUsers: userJoinedAt,
	}

	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("❌ Config JSON xatosi: %v", err)
	} else {
		if err := os.WriteFile(ADMIN_CONFIG_FILE, configData, 0644); err != nil {
			log.Printf("❌ Config faylga yozishda xato: %v", err)
		}
	}

	// ========================
	// 4️⃣ Stats saqlash
	// ========================
	statsMutex.Lock()
	// Bu yerda defer ishlatmaslik yaxshi, chunki funksiya oxiri.
	// Lekin kodingizda bor ekan, turaversin.

	statsData := struct {
		UserJoined  map[int64]time.Time `json:"userJoined"`
		UserActive  map[int64]time.Time `json:"userActive"`
		SearchStats map[string]int      `json:"searchStats"`
	}{
		UserJoined:  userJoined,
		UserActive:  userActive,
		SearchStats: searchStats,
	}

	file, err := json.MarshalIndent(statsData, "", "  ")
	statsMutex.Unlock() // Mutexni iloji boricha tezroq bo'shatamiz

	if err != nil {
		log.Printf("❌ stats.json JSON marshaling xatosi: %v", err)
	} else {
		_ = os.WriteFile("stats.json", file, 0644)
	}
}

func loadData() {
	// ========================
	// 1️⃣ Anime Storage
	// ========================
	if data, err := os.ReadFile(ANIME_STORAGE_FILE); err == nil {
		storageMutex.Lock()
		_ = json.Unmarshal(data, &animeStorage)
		storageMutex.Unlock()
	}

	// ========================
	// 2️⃣ Anime Info
	// ========================
	if data, err := os.ReadFile(ANIME_INFO_FILE); err == nil {
		infoMutex.Lock()
		_ = json.Unmarshal(data, &animeInfo)
		infoMutex.Unlock()
	}
	// 2.1️⃣ Anime Photos (YANGI QO'SHILDI)
	// ========================
	if data, err := os.ReadFile(ANIME_PHOTOS_FILE); err == nil {
		infoMutex.Lock()
		// Agar map e'lon qilinmagan bo'lsa, initialize qilamiz
		if animePhotos == nil {
			animePhotos = make(map[string]string)
		}
		_ = json.Unmarshal(data, &animePhotos)
		infoMutex.Unlock()
		log.Printf("✅ %d ta anime suratlari yuklandi.", len(animePhotos))
	} else {
		log.Printf("⚠️ %s fayli topilmadi yoki o'qib bo'lmadi.", ANIME_PHOTOS_FILE)
	}
	// ========================
	// 3️⃣ Adminlar, Kanallar, Foydalanuvchilar
	// ========================
	if data, err := os.ReadFile(ADMIN_CONFIG_FILE); err == nil {
		// Vaqtinchalik struktura (JSON string kalitlarni o'qish uchun)
		var tempConfig struct {
			Admins   map[string]bool      `json:"Admins"`
			Channels map[string]string    `json:"Channels"`
			AllUsers map[string]time.Time `json:"AllUsers"`
		}

		if err := json.Unmarshal(data, &tempConfig); err == nil {
			// 1. Adminlarni yuklash
			adminIDs = make(map[int64]bool)
			for sID, val := range tempConfig.Admins {
				id, _ := strconv.ParseInt(sID, 10, 64)
				adminIDs[id] = val
			}

			// 2. Kanallarni yuklash (MUHIM QISM)
			channels = make(map[int64]string)
			for sID, val := range tempConfig.Channels {
				id, _ := strconv.ParseInt(sID, 10, 64)
				channels[id] = val
			}

			// 3. Foydalanuvchilarni yuklash
			userJoinedAt = make(map[int64]time.Time)
			for sID, val := range tempConfig.AllUsers {
				id, _ := strconv.ParseInt(sID, 10, 64)
				userJoinedAt[id] = val
				users[id] = true
			}

			log.Printf("✅ %d ta foydalanuvchi va %d ta kanal yuklandi.", len(userJoinedAt), len(channels))
		} else {
			log.Printf("❌ JSON Unmarshal xatosi: %v", err)
		}
	}
	// ========================
	// 4️⃣ Stats yuklash
	// ========================
	if data, err := os.ReadFile("stats.json"); err == nil {
		var statsData struct {
			UserJoined  map[int64]time.Time `json:"userJoined"`
			UserActive  map[int64]time.Time `json:"userActive"`
			SearchStats map[string]int      `json:"searchStats"`
		}
		if err := json.Unmarshal(data, &statsData); err == nil {
			statsMutex.Lock()
			userJoined = statsData.UserJoined
			userActive = statsData.UserActive
			searchStats = statsData.SearchStats
			statsMutex.Unlock()
		}
	}

	log.Printf("✅ %d ta anime va %d ta kanal yuklandi.", len(animeInfo), len(channels))
}

func addUser(userID int64) {
	usersMutex.Lock()
	defer usersMutex.Unlock()

	if userJoinedAt == nil {
		userJoinedAt = make(map[int64]time.Time)
	}
	if allUsers == nil {
		allUsers = make(map[int64]bool)
	}

	if _, exists := userJoinedAt[userID]; !exists {
		userJoinedAt[userID] = time.Now()
		allUsers[userID] = true
		saveData() // darhol saqlash
		saveStats()
	}
}

func saveStats() {
	statsMutex.Lock()
	defer statsMutex.Unlock()

	data := struct {
		UserJoined  map[int64]time.Time `json:"userJoined"`
		UserActive  map[int64]time.Time `json:"userActive"`
		SearchStats map[string]int      `json:"searchStats"`
	}{
		UserJoined:  userJoined,
		UserActive:  userActive,
		SearchStats: searchStats,
	}

	file, _ := json.MarshalIndent(data, "", "  ")
	_ = os.WriteFile("stats.json", file, 0644)
}

func loadAnimePhotos() {
	// 1. Fayl borligini tekshiramiz
	data, err := os.ReadFile(ANIME_PHOTOS_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			// Fayl yo'q bo'lsa, mapni shunchaki ochamiz
			infoMutex.Lock()
			animePhotoMap = make(map[string]string)
			infoMutex.Unlock()
			return
		}
		log.Println("Faylni o'qishda xato:", err)
		return
	}

	// 2. JSONdan mapga o'tkazamiz
	infoMutex.Lock()
	err = json.Unmarshal(data, &animePhotoMap)
	infoMutex.Unlock()

	if err != nil {
		log.Println("JSONni o'qishda xato (Unmarshal):", err)
	} else {
		fmt.Println("✅ Suratlar muvaffaqiyatli yuklandi!")
	}
}

func saveAnimePhotos() {
	infoMutex.Lock()
	// data o'zgaruvchisini marshaldan olamiz
	data, err := json.MarshalIndent(animePhotoMap, "", "  ")
	infoMutex.Unlock()

	if err != nil {
		log.Println("JSON marshallashda xato:", err)
		return
	}

	// DIQQAT: ANIME_PHOTOS_FILE dan keyin qavs bo'lmasligi kerak!
	err = os.WriteFile(ANIME_PHOTOS_FILE, data, 0644)
	if err != nil {
		log.Println("Faylga yozishda xato:", err)
	}
}

func handleAdminText(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID
	text := update.Message.Text
	currentState := adminState[userID]
	// Admin holatini tekshiramiz

	currentState, ok := adminState[userID]

	if !ok {
		return
	}
	if update.Message.Photo != nil {
		if update.Message.Photo != nil && adminState[userID] == "edit_anime_photo_waiting" {
			photos := update.Message.Photo
			photo := photos[len(photos)-1]
			code := animeCodeTemp[userID]

			infoMutex.Lock()
			animePhotoMap[code] = photo.FileID // 1. RAM-ga yozish
			infoMutex.Unlock()

			saveAnimePhotos()

			adminState[userID] = ""
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Rasm JSON-ga muvaffaqiyatli saqlandi!"))
			return
		}
	}
	if strings.HasPrefix(currentState, "anime_reorder:") {
		handleAnimeReorder(bot, update, userID, chatID, currentState)
		return
	}

	switch currentState {

	//case "admin_vip_main":
	//	msg := tgbotapi.NewEditMessageText(chatID, update.CallbackQuery.Message.MessageID, "🌟 VIP Foydalanuvchilar Paneli:")
	//	msg.ReplyMarkup = vipAdminMenu()
	//	bot.Send(msg)

	case "wait_delete_code":
		code := strings.ToLower(strings.TrimSpace(text))
		log.Printf("[DEBUG] Admin %d kod yubordi: '%s'", userID, code)

		storageMutex.RLock()
		items, ok := animeStorage[code]
		name, hasName := animeInfo[code]
		storageMutex.RUnlock()

		if !ok || len(items) == 0 {
			log.Printf("[WARNING] Kod topilmadi yoki qismlar bo'sh: '%s'", code)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday kod topilmadi yoki qismlar yo'q. Qayta yuboring:"))
			return
		}
		if !hasName {
			name = "Nomsiz anime"
		}
		log.Printf("[DEBUG] Anime topildi: '%s', Qismlar soni: %d", name, len(items))

		// Endi holatni o'zgartiramiz
		adminMutex.Lock()
		adminState[userID] = "delete_part_id"
		animeCodeTemp[userID] = code
		adminMutex.Unlock()
		log.Printf("[DEBUG] Admin %d holati 'delete_part_id'ga o'zgartirildi.", userID)

		// Ro'yxatni shakllantirish
		partList := ""
		for i, item := range items {
			partList += fmt.Sprintf("ID: %d | Turi: %s\n", i+1, strings.Title(item.Kind))
		}

		msgText := fmt.Sprintf("🗑 **%s** (%s)\n\nO'chirmoqchi bo'lgan qism ID'sini kiriting:\n\n%s",
			name, strings.ToUpper(code), partList)

		msg := tgbotapi.NewMessage(chatID, msgText)
		msg.ParseMode = "Markdown"

		_, err := bot.Send(msg)
		if err != nil {
			log.Printf("[ERROR] Xabarni yuborib bo'lmadi: %v", err)
			// Agar Markdown xatosi bo'lsa, oddiy matnda yuborish
			msg.ParseMode = ""
			bot.Send(msg)
		} else {
			log.Printf("[DEBUG] Ro'yxat admin ekraniga chiqarildi.")
		}
		return

	case "delete_part_id":
		code := animeCodeTemp[userID]
		input := strings.TrimSpace(text)
		log.Printf("[DEBUG] O'chirish so'rovi: User: %d, Kod: %s, Kiritilgan IDlar: '%s'", userID, code, input)

		// 1. ID'larni parse qilish
		idsToDelete := parseIDsToDelete(input)
		if len(idsToDelete) == 0 {
			log.Printf("[WARNING] User %d noto'g'ri ID formatini kiritdi: '%s'", userID, input)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, o'chirmoqchi bo'lgan qism raqamini kiriting (masalan: 1 yoki 1-5):"))
			return
		}
		log.Printf("[DEBUG] Parse qilingan IDlar: %v", idsToDelete)

		// 2. O'chirish jarayoni
		storageMutex.Lock()
		items := animeStorage[code]
		initialCount := len(items)
		log.Printf("[DEBUG] O'chirishdan oldingi qismlar soni: %d", initialCount)

		// ID'larni teskari tartibda saralash (indeks siljib ketmasligi uchun o'ta muhim)
		sort.Slice(idsToDelete, func(i, j int) bool {
			return idsToDelete[i] > idsToDelete[j]
		})
		log.Printf("[DEBUG] Teskari tartibda saralangan IDlar: %v", idsToDelete)

		deletedCount := 0
		for _, id := range idsToDelete {
			indexToRemove := id - 1 // User 1 deydi, bizda 0-indeks

			if indexToRemove >= 0 && indexToRemove < len(items) {
				log.Printf("[DEBUG] O'chirilmoqda: Indeks [%d] (Foydalanuvchi IDsi: %d)", indexToRemove, id)

				// Go usulida elementni slice'dan o'chirish
				items = append(items[:indexToRemove], items[indexToRemove+1:]...)
				deletedCount++
			} else {
				log.Printf("[WARNING] ID %d topilmadi (Mavjud diapazon: 1-%d)", id, len(items))
			}
		}

		// 3. Storage'ni yangilash
		animeStorage[code] = items
		storageMutex.Unlock()

		if deletedCount > 0 {
			log.Printf("[SUCCESS] %s kodli animedan %d ta qism o'chirildi. Qolgan qismlar: %d", code, deletedCount, len(items))
			go saveData() // 💾 JSON faylga saqlash

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %d ta qism muvaffaqiyatli o'chirildi!", deletedCount)))
		} else {
			log.Printf("[ERROR] Hech qanday qism o'chirilmadi. Kiritilgan IDlar diapazondan tashqarida.")
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Bunday raqamli qismlar topilmadi."))
		}

		// 4. Holatni tozalash
		adminMutex.Lock()
		delete(adminState, userID)
		delete(animeCodeTemp, userID)
		adminMutex.Unlock()
		log.Printf("[DEBUG] User %d holati tozalandi.", userID)
		return

	case "wait_vip_add":
		// 1️⃣ Matnni raqamga (ID) aylantiramiz
		targetID, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato! Iltimos, faqat raqamli ID yuboring."))
			return
		}

		// 2️⃣ VIP muddatini tanlash tugmalarini yuboramiz
		msg := tgbotapi.NewMessage(chatID, "⏳ VIP muddatini tanlang:")
		msg.ReplyMarkup = vipDurationMenu(targetID) // InlineKeyboardMarkup tugmalar bilan
		bot.Send(msg)

		// 3️⃣ Holatni VIP muddat tanlashga o‘zgartiramiz
		adminMutex.Lock()
		adminState[userID] = "wait_vip_duration"
		adminMutex.Unlock()
		return

	case "wait_vip_del":
		targetID, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato! Iltimos, faqat raqamli ID yuboring."))
			return
		}

		vipMutex.Lock()
		if _, exists := vipUsers[targetID]; exists {
			delete(vipUsers, targetID)
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 ID: %d VIP ro'yxatidan o'chirildi!", targetID)))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu ID VIP ro'yxatida topilmadi."))
		}
		vipMutex.Unlock()

		go saveData()
		delete(adminState, userID)
		return

	case "wait_reorder_ids":
		code := animeCodeTemp[userID]
		input := strings.TrimSpace(text)

		// 1. Foydalanuvchi yuborgan ID-larni massivga olamiz (masalan: [5, 1, 4, 2, 3])
		newOrder := parseIDsToDelete(input)

		storageMutex.Lock()
		oldItems := animeStorage[code]

		if len(newOrder) != len(oldItems) {
			storageMutex.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ Xato! Barcha %d ta IDni kiritishingiz kerak.", len(oldItems))))
			return
		}

		// 2. Yangi tartib bo'yicha vaqtinchalik massiv yaratamiz
		var reorderedItems []ContentItem

		for _, id := range newOrder {
			index := id - 1 // foydalanuvchi yozgan 1 -> index 0
			if index >= 0 && index < len(oldItems) {
				// Tanlangan elementni yangi massivga qo'shamiz
				reorderedItems = append(reorderedItems, oldItems[index])
			} else {
				storageMutex.Unlock()
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Noto'g'ri ID: %d", id)))
				return
			}
		}

		// 3. Bazani yangilaymiz
		animeStorage[code] = reorderedItems
		storageMutex.Unlock()

		go saveData()

		bot.Send(tgbotapi.NewMessage(chatID, "✅ Tartib saqlandi! Endi qismlar siz yuborgan ketma-ketlikda chiqadi."))

		delete(adminState, userID)
		delete(animeCodeTemp, userID)

	case "delete_anime_code":
		code := strings.ToLower(strings.TrimSpace(text))

		infoMutex.RLock()
		_, exists := animeInfo[code]
		infoMutex.RUnlock()

		if !exists {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod bo‘yicha anime topilmadi."))
			delete(adminState, userID)
			return
		}

		// 🔥 O‘chiramiz
		infoMutex.Lock()
		delete(animeInfo, code)
		infoMutex.Unlock()

		storageMutex.Lock()
		delete(animeStorage, code)
		storageMutex.Unlock()

		// 📢 MUHIM: Ma'lumotlar o'chirilgandan so'ng saqlash
		go saveData() // 💾 Qo'shildi

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 '%s' kodi bo‘yicha anime o‘chirildi!", strings.ToUpper(code))))
		delete(adminState, userID)
		return

	case "add_channel_wait":
		text := update.Message.Text
		var chatID64 int64
		var err error

		// 1. ChatID yoki Username orqali kanalni topish
		if strings.HasPrefix(text, "-100") {
			chatID64, err = strconv.ParseInt(text, 10, 64)
		} else {
			username := text
			if !strings.HasPrefix(username, "@") {
				username = "@" + username
			}
			// Username orqali kanal ma'lumotini olish
			chat, err := bot.GetChat(tgbotapi.ChatInfoConfig{
				ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: username},
			})
			if err == nil {
				chatID64 = chat.ID
			}
		}

		if err != nil || chatID64 == 0 {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Kanal topilmadi. ID yoki Usernameni tekshiring."))
			return
		}

		// 2. Kanal turini tekshirish
		chat, _ := bot.GetChat(tgbotapi.ChatInfoConfig{
			ChatConfig: tgbotapi.ChatConfig{ChatID: chatID64},
		})

		if chat.Type == "channel" || chat.Type == "supergroup" {
			// Agar kanal MAXFIY bo'lsa (username yo'q bo'lsa)
			if chat.UserName == "" {
				adminState[userID] = "add_private_link"
				adminTempID[userID] = chat.ID // Kanal ID sini vaqtincha saqlaymiz
				bot.Send(tgbotapi.NewMessage(chatID, "🔒 Bu maxfiy kanal ekan. Iltimos, kanalga ulanish havolasini (Invite Link) yuboring:"))
			} else {
				// Agar kanal OCHIQ bo'lsa
				channels[chat.ID] = chat.UserName
				go saveData()
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Ochiq kanal qo'shildi: @%s", chat.UserName)))
				adminState[userID] = ""
			}
		}

	case "add_admin_id":
		newID, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ ID noto‘g‘ri. Faqat raqam kiriting."))
			return
		}
		admins[newID] = true
		go saveData() // 💾 Qo'shildi
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ %d adminlarga qo‘shildi!", newID)))
		delete(adminState, userID)
		return

	case "edit_new_code":
		new_code := strings.ToLower(strings.TrimSpace(text))
		old_code := animeCodeTemp[userID]
		if new_code == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Kod bo‘sh bo‘lishi mumkin emas. Qayta kiriting:"))
			return
		}
		if new_code == old_code {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Yangi kod eski kod bilan bir xil bo‘lishi mumkin emas."))
			return
		}
		// Yangi kod allaqachon mavjudligini tekshirish
		infoMutex.RLock()
		_, exists := animeInfo[new_code]
		infoMutex.RUnlock()

		if exists {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod (ID) allaqachon boshqa animega tegishli!"))
			return
		}

		// 🔥 Ma'lumotlarni yangi kodga ko'chirish
		infoMutex.Lock()
		storageMutex.Lock()

		// 1. Anime nomini yangi kod bilan saqlash
		animeInfo[new_code] = animeInfo[old_code]

		// 2. Anime kontentini yangi kod bilan saqlash
		animeStorage[new_code] = animeStorage[old_code]

		// 3. Eskilarini o'chirish
		delete(animeInfo, old_code)
		delete(animeStorage, old_code)

		storageMutex.Unlock()
		infoMutex.Unlock()

		// 💾 MUHIM: Ma'lumotlar muvaffaqiyatli ko'chirilgandan so'ng saqlash!
		go saveData() // 💾 Qo'shildi

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ anime kodi  ( %s ) -  dan - ( %s ) ga muvaffaqiyatli o'zgartirildi!", strings.ToUpper(old_code), strings.ToUpper(new_code))))
		delete(adminState, userID)
		delete(animeCodeTemp, userID)
		return // ------------------ ADMIN: Admin o'chirish ID so'rov ------------------

	case "edit_new_name":
		new_name := strings.TrimSpace(text)
		old_code, ok := animeCodeTemp[userID]
		if !ok {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato: anime kodi topilmadi. Qayta boshlash uchun /admin buyrug'ini kiriting."))
			delete(adminState, userID)
			return
		}

		if new_name == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Nom bo‘sh bo‘lishi mumkin emas. Qayta kiriting:"))
			return
		}

		// Nomni yangilash
		infoMutex.Lock()
		animeInfo[old_code] = new_name
		infoMutex.Unlock()

		go saveData() // 💾 Qo'shildi

		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ anime nomi ( %s ) ga muvaffaqiyatli o'zgartirildi!", new_name)))

		// Holatni tozalash
		delete(adminState, userID)
		delete(animeCodeTemp, userID)
		return

		// ... yuqoridagi boshqa case'lar (delete_anime_code, add_admin_id va h.k.) ...

		// ⚠️ Kodning boshqa joyida e'lon qilingan bo'lishi kerak:
		// const MAIN_ADMIN_ID int64 = 123456789

	case "remove_admin_id":
		remID, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ ID noto‘g‘ri. Faqat raqam kiriting."))
			return
		}

		if remID == userID {
			bot.Send(tgbotapi.NewMessage(chatID, "⚠️ O‘zingizni o‘chirishingiz mumkin emas!"))
			return
		}

		// 👇👇👇 ASOSIY ADMINNI TEKSHIRISH QO'SHILDI 👇👇👇
		if remID == MAIN_ADMIN_ID {
			bot.Send(tgbotapi.NewMessage(chatID, "🛑 Asosiy adminni o‘chirish mumkin emas!"))
			return
		}
		// 👆👆👆 ASOSIY ADMINNI TEKSHIRISH QO'SHILDI 👆👆👆

		delete(admins, remID)
		go saveData() // 💾 Qo'shildi
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🗑 %d admin o‘chirildi!", remID)))
		delete(adminState, userID)
		return // return qo'shildi

	case "block_user":
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato ID. Raqam kiriting."))
			return
		}
		blockedUsers[id] = true
		go saveData() // 💾 Qo'shildi
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🚫 %d bloklandi!", id)))
		delete(adminState, userID)
		return // return qo'shildi

	case "unblock_user":
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato ID. Raqam kiriting."))
			return
		}
		delete(blockedUsers, id)
		go saveData() // 💾 Qo'shildi
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("♻️ %d blokdan chiqarildi!", id)))
		delete(adminState, userID)
		return // return qo'shildi

	case "add_channel_chatid":
		text = strings.TrimSpace(text)

		// 1️⃣ HAVOLA bo‘lsa → bu ID EMAS
		if strings.Contains(text, "t.me") {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu chatID emas. Iltimos, kanalning raqamli ChatID'sini yuboring (masalan: -1001234567890)."))
			return
		}

		// 2️⃣ faqat raqam bo‘lishi kerak va -100 bilan boshlanishi sharti
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil || !strings.HasPrefix(text, "-100") {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Noto‘g‘ri chatID. ChatID -100 bilan boshlanishi kerak."))
			return
		}

		// 3️⃣ ChatID vaqtinchalik saqlanadi
		adminTempID[userID] = id
		adminState[userID] = "add_channel_username"

		bot.Send(tgbotapi.NewMessage(chatID, "🔗 Endi kanal username yoki havolasini yuboring:"))
		return

	case "add_channel_username":
		username := strings.TrimSpace(text)

		if username == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, kanal username yoki havolasini kiriting!"))
			return
		}

		chatIDnum := adminTempID[userID]

		// Maxfiy havola bo'lsa
		if strings.HasPrefix(username, "https://t.me/+") {
			// Bot kanalga kirmaydi, faqat ro'yxatga qo'shamiz
			adminTempChannels[userID] = append(adminTempChannels[userID], Channel{
				ChatID:   0, // hali raqam yo'q
				Username: username,
			})

			bot.Send(tgbotapi.NewMessage(chatID, "🔗 Guruh havolasi olindi!\n📨 Admin tasdiqlashi kerak."))
			bot.Send(tgbotapi.NewMessage(chatID, "✅ Kanal qo‘shildi!"))
		} else {
			// Oddiy username yoki raqamli ChatID
			channels[chatIDnum] = username
			saveData()
			bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✅ Kanal qo‘shildi!\nChatID: %d\nUsername: %s", chatIDnum, username),
			))
		}

		delete(adminState, userID)
		delete(adminTempID, userID)
		return

	case "remove_channel":
		if strings.HasPrefix(text, "https://t.me/+") {
			// Maxfiy havola bo'lsa
			// ... (Sizning kodingizdagi kabi, adminTempChannels ro'yxatidan o'chirish)
			found := false
			for i, ch := range adminTempChannels[userID] {
				if ch.Username == text {
					adminTempChannels[userID] = append(adminTempChannels[userID][:i], adminTempChannels[userID][i+1:]...)
					found = true
					bot.Send(tgbotapi.NewMessage(chatID, "✅ Maxfiy kanal havolasi o‘chirildi!")) // Xabarni aniqlashtirdik
					break
				}
			}
			if !found {
				bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday maxfiy havola topilmadi."))
			}
			return
		}

		// Raqamli ChatID bo'lsa
		id, err := strconv.ParseInt(text, 10, 64)
		if err == nil { // Matn ChatID raqami sifatida parse qilina olsa

			// 1. Oddiy kanallar xaritasidan o'chirish (agar mavjud bo'lsa)
			if _, ok := channels[id]; ok {
				delete(channels, id)
				saveData()
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Oddiy kanal o‘chirildi! \n(ChatID: %d)", id)))
				return
			}

			// 2. adminTempChannels ro'yxatidan o'chirish (agar maxfiy kanal bo'lsa)
			found := false
			for i, ch := range adminTempChannels[userID] {
				// Agar ro'yxatdagi kanalning ChatID'si so'ralgan ID ga teng bo'lsa
				if ch.ChatID == id {
					adminTempChannels[userID] = append(adminTempChannels[userID][:i], adminTempChannels[userID][i+1:]...)
					found = true
					bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Ro'yxatdagi kanal o‘chirildi! \n (ChatID: %d) ", id)))
					break
				}
			}
			if found {
				return
			}

		}

		// Agar raqam bo'lmasa yoki yuqorida topilmagan bo'lsa, username bo'lishi mumkin
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Bunday kanal topilmadi. Raqamli ChatID yoki to'liq maxfiy havolani kiriting."))
		return

	case "anime_name":
		name := strings.TrimSpace(text)
		if name == "" {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Nomi bo'sh bo'lishi mumkin emas. Iltimos nom kiriting:"))
			return
		}
		animeNameTemp[userID] = name
		adminState[userID] = "anime_code"
		bot.Send(tgbotapi.NewMessage(chatID, "🆔 Endi anime kodi kiriting:"))
		return

	//case "anime_code":
	//	code := strings.ToLower(strings.TrimSpace(text))
	//
	//	if code == "" {
	//		bot.Send(tgbotapi.NewMessage(chatID, "❌ Kod bo'sh bo'lishi mumkin emas."))
	//		return
	//	}
	//
	//	infoMutex.RLock()
	//	_, exists := animeInfo[code]
	//	infoMutex.RUnlock()
	//
	//	if exists {
	//		// MUHIM: Holatni o'zgartirmaymiz!
	//		// adminState[userID] hali ham "anime_code" bo'lib turishi shart.
	//		bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Bu kod allaqachon mavjud! Iltimos, boshqa kod kiriting:"))
	//		return // Bu yerda funksiyadan chiqamiz, foydalanuvchi qayta yozishini kutamiz
	//	}
	//
	//	// Agar kod yangi bo'lsa, saqlaymiz
	//	infoMutex.Lock()
	//	animeInfo[code] = animeNameTemp[userID]
	//	infoMutex.Unlock()
	//
	//	animeCodeTemp[userID] = code
	//
	//	// Endi holatni o'zgartiramiz
	//	adminState[userID] = "anime_photo"
	//	bot.Send(tgbotapi.NewMessage(chatID, "🌌 Zo'r! Endi anime uchun muqova (rasm) yuboring:"))
	//	return

	case "delete_channel":
		text = strings.TrimSpace(text)
		var found bool
		var chatID int64

		// 1. Agar raqam bo'lsa
		id, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			if _, ok := channels[id]; ok {
				delete(channels, id)
				found = true
				chatID = id
			}
		} else {
			// 2. Agar havola (@username) bo'lsa
			for idTemp, username := range channels {
				if username == text || "@"+username == text {
					delete(channels, idTemp)
					found = true
					chatID = idTemp
					break
				}
			}
		}

		// Javob yuborish
		if found {
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Kanal o‘chirildi! ChatID: %d", chatID)))
		} else {
			// Agar topilmasa, javobni adminga yoki default chatga yuborish kerak
			bot.Send(tgbotapi.NewMessage(chatID /* yoki defaultChatID */, "❌ Bunday ChatID yoki havola topilmadi."))
		}
		return

	case "anime_code":

		code := strings.ToLower(strings.TrimSpace(text))
		userID := update.Message.From.ID

		// 1. Asosiy bazaga (animeInfo) yozish - BU QATOR SIZDA YO'Q EDI
		infoMutex.Lock()
		if animeInfo == nil {
			animeInfo = make(map[string]string)
		}
		// Nomi va kodini bog'lab qo'yamiz
		animeInfo[code] = animeNameTemp[userID]
		infoMutex.Unlock()

		// 2. Vaqtinchalik mapga yozish
		animeCodeTemp[userID] = code

		// 3. Keyingi bosqichga o'tish
		adminState[userID] = "anime_photo"
		bot.Send(tgbotapi.NewMessage(chatID, "🌌 Zo'r! Endi muqova yuboring:"))

		// Terminalda tekshirish uchun
		fmt.Printf("✅ Yangi anime bazaga qo'shildi: [%s] -> %s\n", code, animeNameTemp[userID])
		return
	case "anime_photo":
		// 1. Faqat rasm ekanligini tekshiramiz
		if update.Message == nil || update.Message.Photo == nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Iltimos, anime muqovasi uchun rasm yuboring:"))
			return
		}

		// 2. Rasmni olamiz
		photoArray := update.Message.Photo
		photoID := photoArray[len(photoArray)-1].FileID
		code := animeCodeTemp[userID]

		// 3. Rasmni mapga saqlaymiz
		infoMutex.Lock()
		if animePhotos == nil {
			animePhotos = make(map[string]string)
		}
		animePhotos[code] = photoID
		infoMutex.Unlock()

		// 4. Holatni videolarga o'zgartiramiz
		adminState[userID] = "anime_videos"

		storageMutex.RLock()
		videoCount := len(animeStorage[code])
		storageMutex.RUnlock()

		// 5. Siz xohlagan xabar
		txt := fmt.Sprintf(
			"🎬 **Nom:** %s\n🆔 **Kod:** `%s`\n\n🌌 **Muqova alohida saqlandi!**\n📊 **Hozirgi qismlar:** %d ta\n\nEndi **video yoki fayllarni** yuboring. Tugatgach **/ok** deb yozing.",
			animeNameTemp[userID], code, videoCount,
		)

		msg := tgbotapi.NewMessage(chatID, txt)
		msg.ParseMode = "Markdown"
		bot.Send(msg)

		go saveData()
		return

	case "add_private_link":
		link := update.Message.Text
		if !strings.HasPrefix(link, "https://t.me/") {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Noto'g'ri havola. Havola https://t.me/ bilan boshlanishi kerak."))
			return
		}

		targetChatID := adminTempID[userID]
		channels[targetChatID] = link // Maxfiy kanal uchun siz yuborgan havolani saqlaydi
		go saveData()

		bot.Send(tgbotapi.NewMessage(chatID, "✅ Maxfiy kanal va havola muvaffaqiyatli saqlandi!"))
		adminState[userID] = ""
		delete(adminTempID, userID)

	case "edit_anime_code":
		code := strings.ToLower(strings.TrimSpace(text))

		infoMutex.RLock()
		name, exists := animeInfo[code]
		infoMutex.RUnlock()

		if !exists {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Bu kod bo‘yicha anime topilmadi."))
			delete(adminState, userID)
			return
		}

		// 🔥 Qismlar sonini hisoblash
		storageMutex.RLock()
		partCount := len(animeStorage[code])
		storageMutex.RUnlock()

		// ✅ Menyuni yuborish
		msg := tgbotapi.NewMessage(
			chatID,
			fmt.Sprintf("anime: %s (%s)\nMavjud qismlar soni: %d ta.\n\nQuyidagilardan birini tanlang:",
				name, strings.ToUpper(code), partCount), // <-- Qismlar soni qo'shildi
		)
		msg.ParseMode = "Markdown"

		// editMenu ni chaqirish
		msg.ReplyMarkup = editMenu(code, name)

		delete(adminState, userID)
		bot.Send(msg)
		return // ------------------ ADMIN: kontent qabul qilish (TO'G'RILANGAN) ------------------
	// handleAdminText funksiyasi ichidagi switch blokiga qo'shiladi

	// handleAdminText funksiyasi ichidagi switch blokida "anime_videos" holatining yangilangan qismi:
	case "anime_videos":
		code := animeCodeTemp[userID]
		chatID := update.Message.Chat.ID
		text := update.Message.Text

		// 1. Dastlabki tekshiruv va Log
		if code == "" {
			log.Printf("⚠️ [XATO] User %d: Anime kodi topilmadi (State: anime_videos)", userID)
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xatolik: anime kodi topilmadi."))
			delete(adminState, userID)
			return
		}

		// 2. /ok — yuklashni yakunlash
		if strings.ToLower(text) == "/ok" {
			storageMutex.RLock()
			total := len(animeStorage[code])
			storageMutex.RUnlock()

			// TERMINAL LOG
			fmt.Printf("\n--- ✅ YAKUNLASH ---\n")
			fmt.Printf("📂 Kod: %s\n📦 Jami qismlar: %d\n👤 Admin: %d\n-------------------\n", code, total, userID)

			go saveData()

			link := fmt.Sprintf("https://t.me/ergergtr_bot?start=%s", code)
			finishText := fmt.Sprintf(" '%s' uchun yuklash tugadi!\n\n Jami: %d ta qism\n Ko‘rish: %s",
				animeNameTemp[userID], total, link)

			bot.Send(tgbotapi.NewMessage(chatID, finishText))

			// Tozalash logi
			delete(adminState, userID)
			delete(animeCodeTemp, userID)
			delete(animeNameTemp, userID)
			log.Printf("🧹 [INFO] User %d uchun sessiya tozalandi.", userID)
			return
		}

		// 3. Kontentni aniqlash
		var item ContentItem
		var itemKind string
		var isContent = true

		// ... (formatlarni tekshirish qismi o'zgarishsiz qoladi) ...
		// ... video, photo, document, text ...

		// 4. Kontentni saqlash va Terminalda ko'rsatish
		if isContent {
			storageMutex.Lock()
			animeStorage[code] = append(animeStorage[code], item)
			episode := len(animeStorage[code])
			storageMutex.Unlock()

			// TERMINAL LOG (Har bir qism kelganda)
			log.Printf("📥 [YANGI QISIM] Kod: %s | Qism: %d | Turi: %s", code, episode, itemKind)

			msg := fmt.Sprintf("💾 %d-qism qabul qilindi. Turi: %s /ok ", episode, strings.Title(itemKind))
			bot.Send(tgbotapi.NewMessage(chatID, msg))
		}
		return
	//case "anime_videos":
	//	// 1. Dastlabki tekshiruvlar va o'zgaruvchilar
	//	code := animeCodeTemp[userID]
	//	chatID := update.Message.Chat.ID
	//	text := update.Message.Text
	//
	//	// Agar anime kodi bo'lmasa — xato
	//	if code == "" {
	//		bot.Send(tgbotapi.NewMessage(
	//			chatID,
	//			"❌ Xatolik: anime kodi topilmadi. Jarayonni boshidan boshlang.",
	//		))
	//		delete(adminState, userID)
	//		return
	//	}
	//
	//	// 2. /ok — yuklashni yakunlash
	//	if strings.ToLower(text) == "/ok" {
	//		storageMutex.RLock()
	//		total := len(animeStorage[code])
	//		storageMutex.RUnlock()
	//
	//		// Asinxron saqlash
	//		go saveData()
	//
	//		// 🔗 Deep link
	//		link := fmt.Sprintf("https://t.me/ergergtr_bot?start=%s", code)
	//
	//		// Yakuniy xabar
	//		finishText := fmt.Sprintf(
	//			" '%s' uchun yuklash tugadi!\n\n"+
	//				" Jami: %d ta qism\n"+
	//				" Ko‘rish: %s",
	//			animeNameTemp[userID],
	//			total,
	//			link,
	//		)
	//
	//		bot.Send(tgbotapi.NewMessage(chatID, finishText))
	//
	//		// Tozalash
	//		delete(adminState, userID)
	//		delete(animeCodeTemp, userID)
	//		delete(animeNameTemp, userID)
	//		return
	//	}
	//
	//	// 3. Kontentni aniqlash
	//	var item ContentItem
	//	var itemKind string
	//	var isContent = true
	//
	//	if update.Message.Video != nil {
	//		item = ContentItem{
	//			Kind:   "video",
	//			FileID: update.Message.Video.FileID,
	//		}
	//		itemKind = "video"
	//
	//	} else if update.Message.Photo != nil {
	//		p := update.Message.Photo[len(update.Message.Photo)-1]
	//		item = ContentItem{
	//			Kind:   "photo",
	//			FileID: p.FileID,
	//		}
	//		itemKind = "rasm"
	//
	//	} else if update.Message.Document != nil {
	//		item = ContentItem{
	//			Kind:   "document",
	//			FileID: update.Message.Document.FileID,
	//		}
	//		itemKind = "fayl"
	//
	//	} else if update.Message.Text != "" {
	//		item = ContentItem{
	//			Kind: "text",
	//			Text: update.Message.Text,
	//		}
	//		itemKind = "matn"
	//
	//	} else {
	//		isContent = false
	//		bot.Send(tgbotapi.NewMessage(
	//			chatID,
	//			"❌ Noma’lum format! Video, rasm, fayl yoki matn yuboring.",
	//		))
	//		return
	//	}
	//
	//	// 4. Kontentni saqlash
	//	if isContent {
	//		storageMutex.Lock()
	//
	//		animeStorage[code] = append(animeStorage[code], item)
	//		episode := len(animeStorage[code])
	//
	//		storageMutex.Unlock()
	//
	//		// Adminni xabardor qilish
	//		msg := fmt.Sprintf(
	//			"💾 %d-qism qabul qilindi. Turi: %s /ok ",
	//			episode,
	//			strings.Title(itemKind),
	//		)
	//
	//		bot.Send(tgbotapi.NewMessage(chatID, msg))
	//	}
	//
	//	return

	case "broadcast_text":
		broadcastCache[userID] = update.Message // <-- *tgbotapi.Message sifatida saqlaymiz

		bot.Send(tgbotapi.NewMessage(chatID, "⬆️ Reklama tayyor. Hammasi to'g'rimi? (Ha / Yo‘q)"))
		adminState[userID] = "broadcast_confirm"
		return
	case "broadcast_confirm":
		if strings.ToLower(strings.TrimSpace(text)) == "ha" {
			go func(msg *tgbotapi.Message) {
				for id := range allUsers {
					if msg.Text != "" {
						bot.Send(tgbotapi.NewMessage(id, msg.Text))
					} else if msg.Photo != nil {
						photo := msg.Photo[len(msg.Photo)-1]
						bot.Send(tgbotapi.NewPhoto(id, tgbotapi.FileID(photo.FileID)))
					} else if msg.Video != nil {
						bot.Send(tgbotapi.NewVideo(id, tgbotapi.FileID(msg.Video.FileID)))
					} else if msg.Document != nil {
						bot.Send(tgbotapi.NewDocument(id, tgbotapi.FileID(msg.Document.FileID)))
					}
				}
			}(broadcastCache[userID])

			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📢 Reklama barcha foydalanuvchilarga yetkazildi! (%d ta)", len(allUsers))))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Reklama bekor qilindi."))
		}

		delete(broadcastCache, userID)
		delete(adminState, userID)
		return

	default:
		// Agar 'adminState' o'rnatilgan bo'lsa, lekin 'case' mos kelmasa
		bot.Send(tgbotapi.NewMessage(chatID, "❓ Noma'lum holat! Iltimos, qayta urining yoki /admin deb yozing."))
		delete(adminState, userID) // Noto'g'ri holatni tozalash
		return
	}
}

func initQueue(bot *tgbotapi.BotAPI) {
	go processQueue(bot)
}

func handleAnimeReorder(bot *tgbotapi.BotAPI, update tgbotapi.Update, userID int64, chatID int64, currentState string) {
	code := strings.TrimPrefix(currentState, "anime_reorder:")
	newOrder := update.Message.Text

	nums := strings.Split(newOrder, ",")

	storageMutex.Lock()
	oldList := animeStorage[code]

	if len(nums) != len(oldList) {
		storageMutex.Unlock()
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Idlar soni mos emas!"))
		return
	}

	reordered := make([]ContentItem, len(oldList))
	for newIndex, idStr := range nums {
		idStr = strings.TrimSpace(idStr)
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 1 || id > len(oldList) {
			storageMutex.Unlock()
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Xato ID: "+idStr))
			return
		}
		reordered[newIndex] = oldList[id-1]
	}

	animeStorage[code] = reordered
	storageMutex.Unlock()

	bot.Send(tgbotapi.NewMessage(chatID, "✅ Qismlar muvaffaqiyatli tartiblandi!"))
	delete(adminState, userID)
}

func processQueue(bot *tgbotapi.BotAPI) {
	for task := range uploadQueue {
		storageMutex.Lock()
		animeStorage[task.Code] = append(animeStorage[task.Code], task.Item)
		episode := len(animeStorage[task.Code])
		storageMutex.Unlock()

		bot.Send(tgbotapi.NewMessage(task.UserID,
			fmt.Sprintf("💾 %d-qism saqlandi (navbat bilan).", episode)))
	}
}

func parseIDsToDelete(input string) []int {
	var ids []int
	seen := make(map[int]bool) // Takrorlanishlarni oldini olish uchun

	// 1. Kiritilgan matnni vergul (,) bo'yicha ajratamiz
	parts := strings.Split(input, ",")

	for _, part := range parts {
		trimmedPart := strings.TrimSpace(part)
		if trimmedPart == "" {
			continue
		}

		// 2. Agar tire (-) bo'lsa, uni oralig' (range) sifatida tahlil qilamiz
		if strings.Contains(trimmedPart, "-") {
			rangeParts := strings.Split(trimmedPart, "-")
			if len(rangeParts) != 2 {
				continue
			}

			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))

			// Tekshiruv: Raqamlar musbat bo'lishi va start <= end bo'lishi kerak
			if err1 == nil && err2 == nil && start > 0 && end >= start {
				for i := start; i <= end; i++ {
					if !seen[i] {
						ids = append(ids, i)
						seen[i] = true
					}
				}
			}
		} else {
			// 3. Shunchaki bitta ID raqam bo'lsa
			id, err := strconv.Atoi(trimmedPart)
			if err == nil && id > 0 {
				if !seen[id] {
					ids = append(ids, id)
					seen[id] = true
				}
			}
		}
	}
	return ids
}
