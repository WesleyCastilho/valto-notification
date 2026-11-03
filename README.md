# Valto Notification

API independente de notificações, extraída do transporte Evolution utilizado pelo CT BunkerBJJ. O Bunker permanece operando pelo fluxo atual. Esta primeira versão prepara a migração; não altera o ambiente de produção nem ativa um segundo agendador.

## O que já funciona

- Autenticação por cliente e isolamento de lotes, mensagens e tentativas.
- Lotes persistentes no PostgreSQL, envio imediato ou `send_at` em RFC3339, expiração, pausa e cancelamento de pendentes.
- `Idempotency-Key` por pedido e `dedup_key` por mensagem/cliente. Repetir a chave com conteúdo diferente retorna 409.
- Fila com bloqueio de linhas, controle de ritmo compartilhado por URL/instância e recuperação de processos interrompidos.
- Verificação de conexão da Evolution antes de retirar mensagens da fila.
- Tentativas separadas, resposta com ID da Evolution e distinção entre `queued`, `accepted`, `rejected` e `unknown`.
- Espera progressiva para 429 (até três tentativas automáticas); repetição manual apenas de rejeições conhecidas. Resultados incertos nunca são reenviados automaticamente.
- Cliente Go em `sdk` para integração pelo servidor do Bunker.
- Docker, configuração para EasyPanel, documentação de API e testes com PostgreSQL/Evolution simulada.

`accepted` significa aceite pela Evolution com ID de mensagem. Não significa entrega ou leitura. O serviço ainda não recebe confirmações de entrega/leitura.

## Subir para homologação

1. Copie `.env.example` para `.env`, ou configure as mesmas variáveis diretamente no EasyPanel.
2. Configure `DATABASE_URL` e as variáveis existentes `EVOLUTION_API_URL`, `EVOLUTION_API_KEY` e `EVOLUTION_INSTANCE_NAME`. A configuração é lida exclusivamente do ambiente.
3. Mantenha `DELIVERY_ENABLED=false` durante a homologação inicial. Isso permite enfileirar e consultar sem chamar o envio da Evolution.
4. Execute `docker compose up --build -d`, ou siga [EasyPanel](docs/DEPLOYMENT.md).
5. Confira `/healthz`, `/readyz` e o diagnóstico autenticado `/v1/provider/health`.

O exemplo de banco é somente de desenvolvimento local. A Valto usa sua própria base. Não é necessário criar ou montar `clients.json`; `CLIENTS_CONFIG_FILE` não é lido. `.env` e chaves reais ficam fora do Git e da imagem.

O cliente padrão é `bunker`. A autenticação da API Valto reutiliza `EVOLUTION_API_KEY` por padrão, conforme a configuração existente. `CLIENT_ID` e `VALTO_API_KEY` são substituições opcionais, sem exigir novos valores para iniciar.

## Contrato da API

Todas as rotas `/v1` exigem `Authorization: Bearer <chave-do-cliente-valto>`. O cliente autenticado determina o isolamento; não se aceita um `tenant_id` arbitrário no pedido. A API é destinada a servidores; a chave não deve ir para o navegador.

| Método e rota | Finalidade |
| --- | --- |
| `POST /v1/batches` | Criar lote imediato/agendado; exige `Idempotency-Key` |
| `GET /v1/batches/{id}` | Consultar estado e contagens persistentes |
| `PATCH /v1/batches/{id}` | `active`, `paused` ou `canceled` |
| `GET /v1/batches/{id}/messages?page=1` | Mensagens do lote, 50 por página |
| `GET /v1/messages/{id}` | Estado e erro da mensagem |
| `GET /v1/messages/{id}/attempts` | Histórico de tentativas |
| `POST /v1/messages/{id}/retry` | Repetir rejeição elegível, preservando tentativas |
| `GET /v1/provider/health` | Diagnosticar conexão da instância e modo de envio |

Exemplo de corpo; o `recipient` deve incluir o código do país. A aplicação de origem decide o destinatário, as regras e o texto.

```json
{
  "source": "bunker.cadastro-pendencias",
  "messages": [
    {
      "external_id": "id-do-perfil",
      "dedup_key": "bunker:cadastro-pendencias:2026-11-03:id-do-perfil",
      "recipient": "5535999991234",
      "text": "Olá! Você tem pendências no seu cadastro do CT BunkerBJJ."
    }
  ]
}
```

`send_at` e `expires_at` são opcionais, com timezone explícito, por exemplo `2026-11-04T09:00:00-03:00`. Sem `send_at`, o lote fica disponível imediatamente. Nenhum horário depende de alguém abrir uma tela. Ainda não há regras recorrentes configuráveis: a aplicação de origem cria os lotes de cada ocorrência, com chaves estáveis.

Resposta inicial 202 significa **na fila**, com contagens separadas. Repetir um pedido idêntico com a mesma chave retorna 200 e o mesmo lote. Uma nova chave de pedido com `dedup_key` já existente é contabilizada em `suppressed` e não cria outra mensagem. Erros incluem código e `request_id`; não exponha a resposta bruta do provedor.

Limites: 1 MiB por pedido, até 500 mensagens por lote, texto até 10.000 bytes. Para públicos maiores, divida em partes com chaves estáveis por parte. Não altere a chave após timeout.

## Testes

```sh
go test ./...
TEST_DATABASE_URL=postgres://... go test -race ./...
go vet ./...
```

Sem `TEST_DATABASE_URL`, os testes de integração são explicitamente ignorados. No GitHub Actions, PostgreSQL 16 é obrigatório e a suíte executa concorrência, isolamento, idempotência, reinício, tentativas, agendamento e autenticação. O provedor é sempre simulado; os testes não enviam WhatsApp real.

## Migração do Bunker

Leia o [plano de transição e retorno](docs/BUNKER_MIGRATION.md) antes de ativar envios. O corte deve impedir que o executor antigo e a Valto assumam os mesmos destinatários. Uma falha de comunicação com a Valto **não autoriza** enviar pela Evolution diretamente.

## Origem e próxima etapa

O transporte `sendText`, header `apikey`, bloqueio de redirects e classificação de resultados derivam do [CT BunkerBJJ](https://github.com/WesleyCastilho/ctbunker), base `7899e5b6d66a0cc9ca44d89f5ba6c0bfae01e397`. As regras de pendências, telefone como possível WhatsApp, seleção e prévia continuam no Bunker. Não há acesso à tabela `profiles` nesta API.

A próxima etapa é homologar, integrar a fila ao backend Bunker e executar a transferência controlada do histórico/bloqueios. Calendário recorrente, painel próprio, métricas de conversão e confirmação por webhook são evoluções posteriores; não estão apresentados como funcionalidades já entregues.
