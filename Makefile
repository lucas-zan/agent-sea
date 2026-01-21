# Agent Engine Makefile

APP_NAME := sea

.PHONY: build up run help binary links chat clean native skills novel-init outline

build: ## Build Docker images
	docker compose build

up: ## Start Docker services
	docker compose up -d

run: ## One-shot start (remove old services/images → rebuild → start)
	docker compose down --rmi local 2>/dev/null || true
	docker compose build --no-cache
	docker compose up -d
	@echo "✅ Services started"

binary: ## Build binary (Docker Linux)
	docker compose run --rm dev go build -o sea .
	@echo "✅ Binary built: ./sea (Linux)"

native: ## Build native binary (macOS)
	@mkdir -p .gocache .gotmp
	GOCACHE=$$(pwd)/.gocache GOTMPDIR=$$(pwd)/.gotmp go build -o sea .
	@echo "✅ Binary built: ./sea (macOS)"

links: binary ## Create shortcut links (chat, each skill name)
	@cp sea chat 2>/dev/null || docker compose run --rm dev cp sea chat
	@echo "✅ Created: ./chat"
	@for skill in $$(docker compose run --rm dev ./sea skills 2>/dev/null | awk '{print $$2}' | sed 's/://'); do \
		cp sea $$skill 2>/dev/null || docker compose run --rm dev cp sea $$skill; \
		echo "✅ Created: ./$$skill"; \
	done

chat: native ## Start chat (interactive)
	./sea chat

validate: native ## Validate all skills
	./sea validate

skills: native ## List all skills
	./sea skills

# ===== Novel-writing shortcuts =====

novel-init: native ## Create novel project (usage: make novel-init name=book_title)
	@if [ -z "$(name)" ]; then \
		echo "Usage: make novel-init name=book_title [genre=genre]"; \
		exit 1; \
	fi
	./sea run novel-init -a name="$(name)" -a genre="$(genre)"

outline: native ## Create outline (usage: make outline idea=idea [project=project_name])
	@if [ -z "$(idea)" ]; then \
		echo "Usage: make outline idea=idea [project=project_name]"; \
		exit 1; \
	fi
	./sea run outline-create -a idea="$(idea)" -a project="$(project)"

skill: native ## Run any skill (usage: make skill s=skill_name args="key=val key=val")
	@if [ -z "$(s)" ]; then \
		echo "Usage: make skill s=skill_name args=\"key=val key=val\""; \
		exit 1; \
	fi
	./sea run $(s) $(foreach a,$(args),-a $(a))

clean: ## Clean binaries and links
	rm -f sea chat hello-world char-create file-* skill-*
	@echo "✅ Cleaned"

help: ## Show help
	@echo "Common commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make novel-init name=MyNovel genre=Romance"
	@echo "  make outline idea=\"A love story where the second male lead wins\""
	@echo "  make skill s=world-build args=\"project=MyNovel\""

.DEFAULT_GOAL := help
