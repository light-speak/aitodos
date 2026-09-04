package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/capability"
)

type fakeCapabilityService struct {
	catalog capability.Catalog
}

func (service *fakeCapabilityService) List(context.Context) (capability.Catalog, error) {
	return service.catalog, nil
}

func (service *fakeCapabilityService) AddSkill(_ context.Context, input capability.SkillInput) (capability.Skill, error) {
	created := capability.Skill{
		ID: "skill-1", Name: input.Name, SourcePath: input.SourcePath,
		ContentSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Enabled:       true, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	service.catalog.Skills = append(service.catalog.Skills, created)
	return created, nil
}

func (service *fakeCapabilityService) AddMCPServer(_ context.Context, input capability.MCPServerInput) (capability.MCPServer, error) {
	created := capability.MCPServer{
		ID: "mcp-1", Name: input.Name, ConfigName: input.ConfigName,
		Enabled: true, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	service.catalog.MCPServers = append(service.catalog.MCPServers, created)
	return created, nil
}

func (service *fakeCapabilityService) RefreshSkill(_ context.Context, skillID string, version int64) (capability.Skill, error) {
	for index := range service.catalog.Skills {
		if service.catalog.Skills[index].ID == skillID {
			service.catalog.Skills[index].Version = version + 1
			service.catalog.Skills[index].ContentSHA256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
			return service.catalog.Skills[index], nil
		}
	}
	return capability.Skill{}, errors.New("missing skill")
}

func TestCapabilityRoutesCreateAndListProjectCatalog(t *testing.T) {
	service := &fakeCapabilityService{}
	mux := http.NewServeMux()
	RegisterCapabilityRoutes(mux, service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/skills",
		`{"name":"发布检查","source_path":".agents/skills/release"}`, http.StatusCreated).Body.Close()
	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/mcp-servers",
		`{"name":"浏览器","config_name":"playwright"}`, http.StatusCreated).Body.Close()
	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/skills/skill-1/refresh",
		`{"version":1}`, http.StatusOK).Body.Close()
	response := requestJSONResponse(t, server.Client(), http.MethodGet,
		server.URL+"/api/project/capabilities", "", http.StatusOK)
	defer response.Body.Close()
	if len(service.catalog.Skills) != 1 || service.catalog.Skills[0].Version != 2 || len(service.catalog.MCPServers) != 1 {
		t.Fatalf("catalog = %#v", service.catalog)
	}
	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/skills", `{`, http.StatusBadRequest).Body.Close()
	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/mcp-servers", `{`, http.StatusBadRequest).Body.Close()
	requestJSONResponse(t, server.Client(), http.MethodPost, server.URL+"/api/project/capabilities/skills/skill-1/refresh", `{"version":0}`, http.StatusBadRequest).Body.Close()
}
