# Deploy e homologação

## EasyPanel

Crie um serviço novo para `WesleyCastilho/valto-notification`, branch `main`, contexto raiz e Dockerfile da raiz. Porta interna **8080**. Use um domínio novo para a Valto; não altere o domínio nem as variáveis da API Bunker.

Crie PostgreSQL exclusivo do serviço e configure a URL real em `DATABASE_URL`. A aplicação valida a URL antes de conectar. Uma falha de configuração não deve ser contornada apontando para a base Bunker.

Variáveis:

| Variável | Configuração |
| --- | --- |
| `PORT` | `8080` |
| `MODE` | `all` inicialmente; `api` e `worker` permitem separar processos depois |
| `DELIVERY_ENABLED` | `false` até a autorização de corte |
| `SEND_INTERVAL` | `1s` inicialmente; permite de `1s` a `1m` |
| `DATABASE_URL` | Conexão PostgreSQL real e exclusiva da Valto |
| `CLIENTS_CONFIG_FILE` | Caminho absoluto do arquivo privado montado, por exemplo `/run/secrets/clients.json` |

Use um arquivo montado no serviço para a lista de clientes. O usuário do container é UID/GID `10001`; o arquivo precisa permitir leitura a esse usuário. Use `clients.example.json` como estrutura, substituindo os campos de chave. O nome da instância confirmado para o primeiro cliente é **Valto**, no servidor `https://evolution.rigora.com.br`. As credenciais reais não são publicadas neste repositório.

O campo `api_key` do cliente autentica o Bunker na Valto. O campo `evolution.api_key` autentica a Valto na Evolution. São credenciais diferentes.

## Verificações

- `/healthz` prova que o processo responde.
- `/readyz` verifica a conexão com PostgreSQL e informa se os envios estão habilitados.
- `/v1/provider/health`, com a chave do cliente Valto, verifica a conexão da instância. Mesmo com `status=ready`, `delivery_enabled=false` mantém a fila parada.
- Enfileire um lote de homologação com chave estável, consulte o lote e confirme que continua `queued` com delivery desativado.
- Habilite envio somente em ambiente de homologação com destinatário explicitamente autorizado para teste. A CI usa provedor simulado.

Todas as réplicas precisam usar o mesmo banco, configuração de clientes, intervalo de envio e valor de `DELIVERY_ENABLED`. Para suspender um cliente em uma ocorrência específica, pause seu lote; para interromper o processamento global, desative o worker e aguarde as chamadas já iniciadas concluírem.

## Operação

Os logs são JSON no stdout e incluem IDs do cliente, lote, mensagem e tentativa. Não imprimem recipient, texto, API keys, URL do banco ou corpo bruto da Evolution. `request_id` acompanha erros HTTP.

Mensagens `unknown` exigem verificação no provedor. O serviço não oferece um botão para reenviar resultados incertos. Uma interrupção depois de chamar o provedor e antes de salvar o resultado será marcada como `unknown` após a expiração da concessão de processamento.

Pausar ou cancelar impede retirar novas mensagens pendentes daquele lote. Uma chamada já iniciada pode concluir e deve conservar seu resultado. Expiração afeta apenas mensagens ainda na fila.

Para mudar uma credencial ou instância, atualize o arquivo privado e reinicie o serviço. A fila e as tentativas permanecem no banco. Não recrie o banco para corrigir uma configuração.
