# API v1

Autenticação: `Authorization: Bearer <chave-do-cliente-valto>`. Content-Type para corpo: `application/json`. Erros: `{"error":{"code":"...","message":"...","request_id":"..."}}`.

## Criar lote

`POST /v1/batches`, header obrigatório `Idempotency-Key` (até 200 caracteres). Corpo:

```json
{
  "source": "sistema.origem",
  "send_at": "2026-11-04T09:00:00-03:00",
  "expires_at": "2026-11-04T18:00:00-03:00",
  "messages": [
    {"external_id":"perfil-1","dedup_key":"ocorrencia:perfil-1","recipient":"5535999991234","text":"Texto revisado"}
  ]
}
```

`send_at` e `expires_at` são opcionais; a expiração precisa ser futura e posterior ao agendamento quando ambos forem informados. Datas precisam de timezone. O telefone inclui país e só dígitos, com `+` inicial opcional. Não se adivinha país nem responsável.

202 = lote persistido e enfileirado. 200 = repetição idêntica da mesma chave. 409 = mesma chave com outro conteúdo. Uma `dedup_key` existente no cliente é suprimida mesmo quando o lote usa outra `Idempotency-Key`.

Resposta: `id`, `source`, `state`, `created_at`, `suppressed`, `counts`. As contagens são `queued`, `sending`, `accepted`, `rejected`, `unknown`, `canceled`, `expired`. `state` é o controle do lote (`active`, `paused`, `canceled`), não uma promessa de entrega. Um lote sem `queued`/`sending` terminou seu processamento, podendo ter falhas.

## Consultar e controlar

- `GET /v1/batches/{id}` retorna o resumo persistente.
- `PATCH /v1/batches/{id}` recebe `{"state":"paused"}`, `{"state":"active"}` ou `{"state":"canceled"}`. Cancelamento não pode ser desfeito. Chamadas já iniciadas podem concluir.
- `GET /v1/batches/{id}/messages?page=1` retorna `items`, `page`, `page_size=50`.
- `GET /v1/messages/{id}` retorna corpo, destinatário, referência externa, estado, erro, ID da Evolution, quantidade de tentativas e horários.
- `GET /v1/messages/{id}/attempts` retorna `items` com cada tentativa, incluindo o resultado anterior depois de uma repetição.
- `POST /v1/messages/{id}/retry` não precisa de corpo. Apenas rejeição conhecida e elegível pode voltar à fila. 409 protege aceites, resultados incertos, chamadas em andamento, cancelados e expirados.
- `GET /v1/provider/health` retorna `status` (`ready` ou `blocked`), `code`, `reason`, `delivery_enabled`.

Um ID pertencente a outro cliente retorna 404. Chave ausente/inválida retorna 401. Falha de banco retorna erro genérico e `request_id`; a aplicação de origem deve reutilizar a chave inicial para recuperar uma criação de resultado incerto.

## Resultados da Evolution

- `accepted`: resposta HTTP de sucesso com `key.id` gravado.
- `rejected`: HTTP 4xx, exceto 408. Motivo fixo e seguro, sem corpo bruto.
- `unknown`: timeout, rede, HTTP 5xx/408/redirect ou sucesso sem ID de mensagem. Exige reconciliação; não pode ser repetido pelo endpoint normal.
- HTTP 429: aguarda `Retry-After` numérico quando presente ou espera progressiva. Máximo de três tentativas automáticas, incluindo a inicial. O ritmo por instância ainda é aplicado.
- Instância desconectada ou consulta de saúde rejeitada: a fila permanece pendente e o diagnóstico registra o bloqueio; não transforma todo o público em tentativas falhas.
