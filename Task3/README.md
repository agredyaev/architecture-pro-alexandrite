# Distributed Tracing MVP

Минимальный стенд для Jaeger + двух Go-сервисов с OpenTelemetry.

## Шаги

```bash
make build        # собрать бинарники и Docker-образы
make import-k3s   # (если кластер k3s) импортировать образы в containerd
make up           # развернуть Jaeger и сервисы в Kubernetes
make test         # smoke-тест: сверка ответа service-a и trace в Jaeger
```

## Полезные команды

```bash
make full          # build -> up -> test (импорт нужно делать отдельно)
make status        # состояние подов в demo и observability
make down          # удалить развернутые ресурсы
```

## Примечания

- Образы используют переменную `IMAGE_REPO` (по умолчанию `localhost`, без завершающего `/`). Для minikube/Kind задайте registry перед целями: `IMAGE_REPO=docker.io/<user> make build up`. Цель `make up` использует `envsubst`, убедитесь, что утилита доступна.
- Для k3s выполните `make import-k3s` после сборки (или импортируйте вручную). После ручного импорта запускайте `SKIP_K3S_IMPORT=1 make full`.
  ```bash
  IMAGE_REPO=localhost # или значение, использованное при сборке
  docker save ${IMAGE_REPO}/service-a:latest | sudo k3s ctr images import -
  docker save ${IMAGE_REPO}/service-b:latest | sudo k3s ctr images import -
  ```
- Smoke-тест (`make test`) делает HTTP-запрос к service-a, затем сверяет `order_id`, `user_id`, `status` с последним trace в Jaeger.
