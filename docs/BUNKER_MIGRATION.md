# Transição CT BunkerBJJ → Valto

## Estado desta entrega

O código operacional do Bunker não é removido nem alterado pela criação deste repositório. Seus envios, prévia, seleção, rotina diária e histórico continuam funcionando pelo executor atual. A Valto pode ser implantada em paralelo com `DELIVERY_ENABLED=false` e banco próprio.

A extração separa o transporte e a execução. O Bunker continua responsável por:

- Regras de cadastro, pendências, academias, perfis ativos/inativos e autorização administrativa.
- Escolha do telefone próprio como possível WhatsApp quando o campo explícito está vazio.
- Público revisado, texto e dados de cada mensagem.
- Data de ocorrência em `America/Sao_Paulo` e intervalo entre lembretes.

A Valto recebe destinatário, texto, identificador externo e chave de deduplicação. Não recebe acesso à tabela de perfis ou à conexão PostgreSQL do Bunker.

## Contrato de integração preparado

O pacote `github.com/WesleyCastilho/valto-notification/sdk` usa somente a biblioteca padrão e pode ser chamado pelo backend Go. O SDK nunca troca automaticamente para outro remetente após erro de rede.

```go
client := sdk.Client{URL: notificationURL, APIKey: notificationClientKey}
batch, err := client.Enqueue(ctx, stableRequestKey, sdk.BatchRequest{
    Source: "bunker.cadastro-pendencias",
    Messages: []sdk.Message{{
        ExternalID: profileID,
        DedupKey: sdk.BunkerDailyKey(dayInSaoPaulo, profileID),
        Recipient: normalizedContact,
        Text: reviewedMessage,
    }},
})
```

`batch.Counts.Queued` é fila. Só `Accepted` corresponde ao aceite pela Evolution. Essa diferença impede reutilizar a antiga assinatura síncrona `Send(...) error` de modo que um simples enfileiramento seja contado como envio.

As futuras rotas Bunker podem continuar atendendo o mesmo frontend, traduzindo o estado persistente da Valto. O código do público e da prévia permanece no Bunker. Use IDs de lote para acompanhar a execução, incluindo depois de um reinício.

## Plano de corte (ainda não executado)

1. Homologar a Valto separadamente, com testes de reinício, rejeição da Evolution e comparação de contagens.
2. Implementar no Bunker uma escolha explícita de executor, com padrão `local`. Tanto o envio manual quanto a rotina diária devem consultar a mesma escolha. Não basta desligar só o agendamento.
3. Preparar a transferência das reservas e histórico existentes. Importação, reconciliação e comparação são uma etapa própria e ainda não estão implementadas nesta primeira extração. Não ativar produção antes dessa etapa.
4. Suspender novos lotes Bunker e aguardar os atuais terminarem. Se uma chamada tiver resultado incerto, tratar como bloqueada até reconciliação.
5. Copiar e validar as reservas do período de corte, inclusive tentativas aceitas/incertas e repetições. Preservar os registros originais. Não converter uma tentativa incerta em falha reenviável.
6. Ativar o executor Valto no Bunker e habilitar o worker Valto em um corte único. O scheduler antigo não pode continuar produzindo envios diretos; se continuar produzindo público, deve apenas criar lotes na Valto.
7. Verificar um lote controlado, IDs de mensagem da Evolution, números de aceites e ausência de duplicidade. Liberar a rotina após essa verificação.

O histórico antigo pode continuar disponível no Bunker durante a transição, com a origem indicada. A transferência posterior precisa preservar horário, texto, destinatário e resultado de cada tentativa, sem recriar mensagens pendentes a partir de histórico já enviado.

## Retorno controlado

Antes de voltar ao executor local: interromper novas submissões à Valto, pausar/cancelar pendentes e aguardar as mensagens em andamento. Transferir de volta as reservas aceitas/incertas geradas pela Valto e só depois reativar o executor local. Uma indisponibilidade de HTTP não indica que o lote não foi criado; repetir a mesma chave e consultar antes de decidir.

## Limites atuais e evolução

Esta versão implementa agendamento de lotes para instantes definidos. Recorrência por calendário, janela de silêncio, consentimento/preferências, webhooks de entrega/leitura, painel próprio e ferramentas de importação são entregas seguintes. Isso evita ligar uma migração incompleta sob a aparência de troca transparente.
