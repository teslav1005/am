package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// النماذج المدعومة
var models = []string{
	"mistral",
	"gemini-fast",
	"minimax",
	"claude-fast",
	"kimi",
	"polly",
	"perplexity-reasoning",
}

// النطاقات المسموح بها
var allowedOrigins = map[string]bool{
	"https://afnanai.com":         true,
	"http://service.afnanai.com":  true,
	"https://service.afnanai.com": true,
}

// بسيط جداً لتتبع عدد الطلبات في الدقيقة (في الذاكرة لـ Serverless)
// ملاحظة: في Vercel Serverless، هذا المتغير قد لا يستمر بين الطلبات المختلفة، 
// لكنه يوفر حماية أساسية داخل نفس الـ Instance.
var lastRequestTime time.Time
var requestCount int

const (
	MaxRequestsPerMinute = 10
	SystemPrompt         = `Act like a realistic human thinker, not an assistant.
concise. Default to 1-2 sentences unless necessary.
No filler. No repetition. No over-explaining.
Before answering, silently check: “Would a rational human say this?”
If you don't know the answer, say: “I don't know."
Do not guess. Do not fabricate information.
Tone: calm, neutral, reserved.
No enthusiasm. No emotion. No flattery.
Keep sentences simple and short.
You are part of Afnan AI company. You are trained on a massive dataset and use a set of internal agents. This is all you know about yourself.`
)

func Handler(w http.ResponseWriter, r *http.Request) {
	// 1. التحقق من النطاق (CORS)
	origin := r.Header.Get("Origin")
	if !allowedOrigins[origin] && origin != "" {
		http.Error(w, "Access Denied: Unauthorized Domain", http.StatusForbidden)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. تحديد عدد الطلبات (Rate Limiting)
	now := time.Now()
	if now.Sub(lastRequestTime) > time.Minute {
		requestCount = 0
		lastRequestTime = now
	}
	if requestCount >= MaxRequestsPerMinute {
		http.Error(w, "Too Many Requests: 10 requests per minute limit", http.StatusTooManyRequests)
		return
	}
	requestCount++

	// 3. قراءة الطلب
	var reqBody struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid Request Body", http.StatusBadRequest)
		return
	}

	// 4. اختيار النموذج تلقائياً إذا كان المختار هو afnanpro
	selectedModel := reqBody.Model
	if selectedModel == "afnanpro" {
		rand.Seed(time.Now().UnixNano())
		selectedModel = models[rand.Intn(len(models))]
	}

	// 5. تجهيز الطلب لـ Pollinations AI
	apiKey := os.Getenv("POLLINATIONS_API_KEY")
	pollinationsURL := "https://gen.pollinations.ai/v1/chat/completions"

	// إضافة System Prompt في بداية المحادثة
	finalMessages := append([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{{Role: "system", Content: SystemPrompt}}, reqBody.Messages...)

	pollinationsReqBody := map[string]interface{}{
		"model":    selectedModel,
		"messages": finalMessages,
		"stream":   true,
	}

	jsonData, _ := json.Marshal(pollinationsReqBody)
	pReq, _ := http.NewRequest("POST", pollinationsURL, bytes.NewBuffer(jsonData))
	pReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		pReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(pReq)
	if err != nil {
		http.Error(w, "Error connecting to AI Provider", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 6. دعم البث المباشر (Streaming)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}

		if strings.TrimSpace(line) == "" {
			continue
		}

		// إرسال البيانات مباشرة للعميل (توكن توكن)
		fmt.Fprint(w, line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}
