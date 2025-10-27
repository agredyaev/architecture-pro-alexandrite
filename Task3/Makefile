.DEFAULT_GOAL := help

KUBECTL ?= kubectl
JAEGER_MANIFEST := k8s/jaeger.yaml
SERVICES_MANIFEST := k8s/services.yaml
IMAGE_REPO ?= localhost
SERVICE_A_IMAGE := $(IMAGE_REPO)/service-a:latest
SERVICE_B_IMAGE := $(IMAGE_REPO)/service-b:latest
SKIP_K3S_IMPORT ?= 0

.PHONY: build import-k3s up full test status down help

##@ Сборка
build: ## Собрать образы сервисов
	@docker build -t $(SERVICE_A_IMAGE) service-a
	@docker build -t $(SERVICE_B_IMAGE) service-b

##@ Импорт
import-k3s: ## Импортировать образы в k3s containerd (нужен sudo)
	@if [ "$(SKIP_K3S_IMPORT)" = "1" ] || ! command -v k3s >/dev/null 2>&1; then \
		echo "k3s не обнаружен или импорт отключен (SKIP_K3S_IMPORT=1)"; \
		exit 0; \
	fi
	@if ! sudo -v; then \
		echo "Не удалось получить права sudo"; \
		echo "Можно импортировать вручную:"; \
		echo "  docker save $(SERVICE_A_IMAGE) | sudo k3s ctr images import -"; \
		echo "  docker save $(SERVICE_B_IMAGE) | sudo k3s ctr images import -"; \
		exit 1; \
	fi
	tmp_a=$$(mktemp) && tmp_b=$$(mktemp) && \
	docker save $(SERVICE_A_IMAGE) -o $$tmp_a && \
	docker save $(SERVICE_B_IMAGE) -o $$tmp_b && \
	sudo k3s ctr images import $$tmp_a && \
	sudo k3s ctr images import $$tmp_b && \
	rm -f $$tmp_a $$tmp_b && \
	echo "Импорт образов завершен"

##@ Развертывание
up: ## Применить манифесты и дождаться готовности
	$(KUBECTL) apply -f $(JAEGER_MANIFEST)
	IMAGE_REPO=$(IMAGE_REPO) envsubst < $(SERVICES_MANIFEST) | $(KUBECTL) apply -f -
	$(KUBECTL) wait --for=condition=available deployment/simplest -n observability --timeout=120s
	$(KUBECTL) wait --for=condition=available deployment/service-a -n demo --timeout=120s
	$(KUBECTL) wait --for=condition=available deployment/service-b -n demo --timeout=120s

##@ Полный цикл
full: build up test ## Сборка, разворачивание и smoke-тест
	@echo "Full pipeline complete"

##@ Smoke-тест
test: ## Проверить трассировку и соответствие trace→response
	@./scripts/smoke_test.py $(KUBECTL)

##@ Просмотр
status: ## Показать состояние подов
	$(KUBECTL) get pods -n demo
	$(KUBECTL) get pods -n observability

port-forward: ## Проброс Jaeger UI и автозапуск браузера
	@bash -c 'set -euo pipefail; \
		echo "Пробрасываю svc/simplest-query в namespace observability на localhost:16686..."; \
		$(KUBECTL) port-forward -n observability svc/simplest-query 16686:16686 >/tmp/jaeger-port-forward.log 2>&1 & \
		pf_pid=$$!; \
		cleanup() { kill $$pf_pid >/dev/null 2>&1 || true; }; \
		trap cleanup EXIT INT TERM; \
		for i in $$(seq 1 10); do \
			if curl -fsS http://localhost:16686 >/dev/null 2>&1; then break; fi; \
			sleep 1; \
		done; \
		if command -v xdg-open >/dev/null 2>&1; then \
			xdg-open http://localhost:16686 >/dev/null 2>&1 || true; \
		elif command -v open >/dev/null 2>&1; then \
			open http://localhost:16686 >/dev/null 2>&1 || true; \
		fi; \
		echo "Jaeger UI доступен на http://localhost:16686 (Ctrl+C для завершения)."; \
		wait $$pf_pid'

##@ Очистка
down: ## Удалить ресурсы
	-IMAGE_REPO=$(IMAGE_REPO) envsubst < $(SERVICES_MANIFEST) | $(KUBECTL) delete --ignore-not-found=true -f -
	-$(KUBECTL) delete -f $(JAEGER_MANIFEST)

##@ Помощь
help: ## Показать список целей
	@awk 'BEGIN {FS=":.*##"; OFS=""} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0,5); next} \
		/^[a-zA-Z0-9_.-]+:.*##/ {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
