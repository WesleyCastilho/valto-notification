# Deploy e homologação

## EasyPanel

Configure o repositório `WesleyCastilho/valto-notification`, branch `main`, contexto raiz e Dockerfile da raiz. Porta interna **8080**. As configurações são lidas diretamente das variáveis de ambiente do serviço.

| Variável | Valor/configuração |
| --- | --- |
| `PORT` | `8080` |
| `DATABASE_URL` | URL real do banco PostgreSQL da Valto |
| `EVOLUTION_API_URL` | `https://evolution.rigora.com.br` |
| `EVOLUTION_API_KEY` | Mesma chave da Evolution já existente, configurada no ambiente |
| `EVOLUTION_INSTANCE_NAME` | `Valto` |
| `MODE` | `all` por padrão; `api` e `worker` permitem separar processos |
| `DELIVERY_ENABLED` | `false` inicialmente, até o corte de produção |
| `SEND_INTERVAL` | `1s` por padrão; aceita de `1s` a `1m` |

Não é necessário `CLIENTS_CONFIG_FILE` nem montagem de arquivo JSON. Uma variável antiga com esse nome é ignorada e pode ser removida. Não há credenciais reais no código.

O cliente padrão é `bunker`, e a chave Bearer da API reutiliza o valor de `EVOLUTION_API_KEY`. Se necessário no futuro, `CLIENT_ID` e `VALTO_API_KEY` permitem substituir o identificador e a chave da API Valto. Essas duas variáveis são opcionais.

O banco `valto_notification` precisa existir, e o usuário da conexão precisa ter acesso. Não substitua a URL por marcadores de exemplo.

## Verificações

- `/healthz` confirma que o processo responde.
- `/readyz` verifica PostgreSQL e informa se o envio está habilitado.
- `/v1/provider/health`, autenticado com a chave acima, verifica a instância.
- `DELIVERY_ENABLED=false` permite enfileirar e consultar sem enviar mensagens, mesmo com a Evolution conectada.
- A ativação da Valto precisa ser coordenada com o executor antigo do Bunker conforme o plano de migração.

Todas as réplicas devem usar o mesmo banco, ambiente e intervalo de envio. Atualize as variáveis e refaça o deploy para trocar credenciais ou instância. A fila e as tentativas continuam no banco.

## Operação

Logs JSON incluem IDs de cliente, lote, mensagem, tentativa e requisição. Não imprimem credenciais, telefone, texto ou corpo bruto da Evolution.

Resultados `unknown` exigem reconciliação, sem reenvio automático. Pausa e cancelamento interrompem a retirada de novos pendentes; chamadas já iniciadas podem concluir. Expiração só afeta mensagens na fila.
