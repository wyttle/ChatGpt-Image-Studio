package api

import (
	"net/http"

	"chatgpt2api/handler"
)

// buildImageResponse converts ImageResults to the OpenAI-compatible response format.
// Only includes url/b64_json and revised_prompt — no internal ChatGPT fields.
func buildImageResponse(r *http.Request, client imageDownloader, results []handler.ImageResult, responseFormat string, sourceAccountID string, cacheDir string) []map[string]any {
	data := make([]map[string]any, 0, len(results))
	for _, img := range results {
		item := map[string]any{
			"revised_prompt":    img.RevisedPrompt,
			"file_id":           img.FileID,
			"gen_id":            img.GenID,
			"conversation_id":   img.ConversationID,
			"parent_message_id": img.ParentMsgID,
		}
		if sourceAccountID != "" {
			item["source_account_id"] = sourceAccountID
		}
		if responseFormat == "b64_json" {
			b64, err := client.DownloadAsBase64(r.Context(), img.URL)
			if err != nil {
				item["url"] = img.URL
			} else {
				item["b64_json"] = b64
			}
		} else {
			filename, err := downloadAndCache(client, img.URL, cacheDir)
			if err != nil {
				item["url"] = img.URL
			} else {
				item["url"] = gatewayImageURL(r, filename)
			}
		}
		data = append(data, item)
	}
	return data
}
