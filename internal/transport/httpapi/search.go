package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/storage"
)

type searchHandler struct {
	store *storage.SearchStore
}

// RegisterSearchRoutes 注册当前项目的有界全文搜索端点。
func RegisterSearchRoutes(mux *http.ServeMux, store *storage.SearchStore) {
	handler := &searchHandler{store: store}
	mux.HandleFunc("GET /api/search", handler.search)
}

func (handler *searchHandler) search(response http.ResponseWriter, request *http.Request) {
	input, err := parseSearchQuery(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_SEARCH", err.Error())
		return
	}
	page, err := handler.store.Search(request.Context(), input)
	if err != nil {
		if validationErr := input.Validate(); validationErr != nil || errors.Is(err, storage.ErrInvalidSearchCursor) {
			writeError(response, http.StatusBadRequest, "INVALID_SEARCH", err.Error())
			return
		}
		writeError(response, http.StatusInternalServerError, "SEARCH_FAILED", "搜索项目内容失败")
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func parseSearchQuery(request *http.Request) (search.Query, error) {
	values := request.URL.Query()
	limit := 20
	var err error
	if value := strings.TrimSpace(values.Get("limit")); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			return search.Query{}, err
		}
	}
	onlyCurrent := false
	if value := strings.TrimSpace(values.Get("only_current")); value != "" {
		onlyCurrent, err = strconv.ParseBool(value)
		if err != nil {
			return search.Query{}, err
		}
	}
	input := search.Query{
		Text: strings.TrimSpace(values.Get("q")), Statuses: splitSearchValues(values["status"]),
		OnlyCurrent: onlyCurrent, Limit: limit, Cursor: values.Get("cursor"),
	}
	if input.UpdatedAfter, err = parseSearchTime(values.Get("updated_after")); err != nil {
		return search.Query{}, err
	}
	if input.UpdatedBefore, err = parseSearchTime(values.Get("updated_before")); err != nil {
		return search.Query{}, err
	}
	for _, value := range splitSearchValues(values["kind"]) {
		input.Kinds = append(input.Kinds, search.Kind(value))
	}
	input = input.Normalized()
	return input, input.Validate()
}

func parseSearchTime(value string) (time.Time, error) {
	if value = strings.TrimSpace(value); value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func splitSearchValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}
