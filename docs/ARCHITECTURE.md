# Arquitetura

## Visão geral

O projeto separa interface, orquestração e integrações externas. A GUI e o CLI constroem uma `config.Config` e delegam a execução a `pipeline.Pipeline`. O pipeline não depende de Fyne e pode ser testado ou executado em ambientes headless.

```text
GUI / CLI
    │
    ▼
pipeline.Pipeline
    ├── environment  → Python/venv, Whisper, Piper e vozes
    ├── audio        → FFmpeg, ffprobe e manipulação PCM/WAV
    ├── transcription→ Whisper local
    ├── translation  → API OpenAI-compatible
    └── tts          → Piper, agrupamento e ajuste temporal
```

## Decisões principais

### Go como orquestrador

Processos, arquivos, rede, cancelamento e interface são controlados em Go. Whisper e Piper são mantidos no runtime Python porque suas distribuições oficiais e modelos já seguem esse ecossistema. Essa fronteira reduz código duplicado e mantém compatibilidade com o projeto Python.

### Pipeline observável

`pipeline.Observer` recebe logs e mudanças de estado. O CLI escreve no terminal; a GUI agenda as atualizações na thread do Fyne. O núcleo permanece desacoplado da apresentação.

### Artefatos determinísticos

`audio.BuildPaths` centraliza os nomes dos intermediários. Isso mantém compatibilidade com o projeto de referência e permite retomar etapas no CLI.

### Tradução tolerante

O cliente aceita endpoints com ou sem `/v1`, detecta automaticamente o modelo e preserva o original quando uma linha numerada não volta na resposta. As escritas de SRT são atômicas.

### Sincronização TTS

1. Cues próximos são agrupados para melhorar a prosódia.
2. O texto é normalizado por idioma.
3. São testados vários valores de `length_scale` do Piper.
4. A tentativa mais próxima da janela é selecionada.
5. Uma correção `atempo` pequena é aplicada quando necessário.
6. O trecho é preenchido ou cortado para ocupar exatamente a janela.
7. Silêncios e trechos são concatenados em PCM16 mono antes da codificação final.

### Cancelamento

No Unix, os subprocessos são iniciados em um grupo próprio; cancelar o contexto encerra o grupo. Isso evita deixar FFmpeg, pip, Whisper ou Piper executando após o cancelamento da GUI.

### Portabilidade

- Caminhos e substituição de arquivos tratam diferenças do Windows.
- O CLI headless não importa Fyne.
- A tag `ci` usa o driver de software do Fyne para testes sem servidor gráfico.
- A GUI nativa usa o driver padrão da plataforma.

## Pontos de extensão

- Novos idiomas: adicionar entrada em `internal/language/language.go`.
- Novo tradutor: implementar outro cliente e injetá-lo no pipeline.
- Novos formatos de legenda: estender `internal/srt`.
- Preservação de múltiplas faixas: alterar a política de mapeamento em `audio.MergeVideoAudio`.
- Aceleração Whisper: adaptar o script incorporado em `internal/transcription` ou criar outro backend.
