# Plano de Empacotamento Autocontido — AI-Video-Dubber-go

## Objetivo

Disponibilizar o `AI-Video-Dubber-go` como um aplicativo `.app` para macOS que abre com dois cliques e não depende de Python, FFmpeg, variáveis de ambiente ou terminal. O bundle deve conter o binário Go, a interface Fyne, o runtime Python com Whisper e Piper, e os binários `ffmpeg` e `ffprobe`.

## Status da implementação

- A resolução de runtime embarcado foi implementada em `internal/config`, com suporte a `.app` e ao tarball headless do CLI.
- `executil.Runner` agora injeta os diretórios embarcados no `PATH` e redireciona chamadas a `ffmpeg` e `ffprobe` para os binários do bundle.
- `internal/environment` pula a criação de `.venv` quando o Python embarcado está em uso e valida que Whisper e Piper estão importáveis.
- `scripts/package-macos.sh` gera `dist/AI-Video-Dubber.app` e `dist/AI-Video-Dubber-cli-darwin-<arch>.tar.gz`.
- `make package-macos` executa o empacotamento completo; `make package-cli` gera apenas o tarball do CLI.
- O script aceita `FFMPEG_BIN` e `FFPROBE_BIN` para builds arm64 nativos quando os binários baixados por padrão não forem adequados.
- Ainda é necessário validar o `.app` em uma máquina limpa sem Python/FFmpeg no `PATH` antes de distribuir para usuários finais.

## Estado atual

- O núcleo do aplicativo é Go/Fyne.
- O CLI e a GUI compartilham a camada `pipeline`.
- O app não embute scripts Python via `go:embed`; em vez disso, ele cria um ambiente virtual `.venv` automaticamente na primeira execução e instala `openai-whisper` e `piper-tts`.
- Depende de `ffmpeg` e `ffprobe` instalados no `PATH`.
- Variáveis de ambiente `PYTHON_BIN`, `VENV_DIR` e `AI_VIDEO_DUBBER_HOME` controlam o runtime.
- Existem dois binários: `ai-video-dubber` (GUI + CLI) e `ai-video-dubber-cli` (headless).
- O target `fyne package` existe no `Makefile`, mas gera apenas o binário Go com metadados; não leva Python nem FFmpeg embarcados.

## Decisões de arquitetura

### 1. Python relocável

Usar como base o **Python standalone** do projeto `indygreg/python-build-standalone`. Essa distribuição já é compilada para ser relocável no macOS.

Pacotes a instalar no Python embarcado:

- `openai-whisper`
- `piper-tts`
- `wheel`, `setuptools`, `pip` atualizados

A instalação deve ser feita diretamente no Python relocável, sem `venv` interno. Isso simplifica o startup e evita a etapa de criação de ambiente virtual na primeira execução.

### 2. FFmpeg embarcado

Baixar builds estáticos de `ffmpeg` e `ffprobe` para macOS (por exemplo, via `evermeet.cx` ou via `brew` copiado para dentro do bundle). Os binários ficam em `Contents/Resources/ffmpeg/`.

### 3. Estrutura do `.app`

```text
AI-Video-Dubber.app/
├── Contents/
│   ├── Info.plist
│   ├── MacOS/
│   │   └── ai-video-dubber
│   └── Resources/
│       ├── icon.png
│       ├── python/
│       │   ├── bin/python3
│       │   └── lib/python3.12/site-packages/
│       │       ├── whisper/
│       │       ├── piper/
│       │       └── ...
│       └── ffmpeg/
│           ├── ffmpeg
│           └── ffprobe
```

### 4. Descoberta de recursos em runtime

Adicionar uma função Go que, a partir de `os.Executable()`, detecta se o binário está dentro de um `.app` e resolve caminhos relativos para:

- `Contents/Resources/python/bin/python3`
- `Contents/Resources/ffmpeg/ffmpeg`
- `Contents/Resources/ffmpeg/ffprobe`

A ordem de resolução deve ser:

1. Variáveis de ambiente `PYTHON_BIN`, `VENV_DIR` e `ffmpeg` no `PATH` (modo desenvolvimento).
2. Recursos bundled em `Contents/Resources/`.
3. Fallback para `PATH` do sistema.

### 5. CLI headless

O binário `ai-video-dubber-cli` também pode ser empacotado como executável autocontido, mas o foco principal é o `.app` da GUI. Para o CLI, pode-se gerar um tarball com o binário Go, Python e FFmpeg lado a lado, usando a mesma lógica de descoberta de recursos.

### 6. Cache de modelos

Modelos Whisper e vozes Piper continuarão sendo baixados na primeira execução e armazenados em `~/Library/Caches/AI-Video-Dubber` ou equivalente. Isso mantém o bundle menor e permite atualizar modelos sem rebuild.

## Mudanças necessárias

### Código Go

- `internal/config/config.go`
  - Adicionar função auxiliar para resolver caminhos de recursos embarcados.
  - Preservar leitura das variáveis de ambiente existentes.
  - Ajustar `VenvDir` para não ser criado quando Python bundled estiver disponível.

- `internal/environment/setup.go`
  - Pular criação de `.venv` quando `pythonExe` já for o Python embarcado.
  - Garantir que `whisper` e `piper` estejam importáveis no Python bundled.

- `internal/audio/ffmpeg.go`
  - Usar `ffmpeg` e `ffprobe` embarcados quando detectados.
  - Manter fallback para `PATH`.

- `internal/executil/runner.go`
  - Garantir que subprocessos enxerguem o Python e FFmpeg embarcados.

### Build e scripts

- Criar `scripts/package-macos.sh`.
  - Baixar Python standalone para a arquitetura alvo.
  - Instalar `openai-whisper` e `piper-tts`.
  - Baixar `ffmpeg` e `ffprobe` estáticos.
  - Compilar `cmd/ai-video-dubber` e `cmd/ai-video-dubber-cli`.
  - Montar a estrutura do `.app`.
  - Gerar `Info.plist`.
  - Copiar ícone de `assets/icon.png`.
  - Opcional: assinar com `codesign` e notarizar com `notarytool`.

- Atualizar `Makefile`.
  - Adicionar target `package-macos`.
  - Adicionar target `package-cli` para tarball autocontido do CLI.

## Fases de execução

### Fase 1 — Análise do código

- Mapear todos os pontos onde `PYTHON_BIN`, `VENV_DIR`, `ffmpeg`, `ffprobe` e `python` são usados.
- Confirmar dependências exatas de Whisper e Piper.
- Verificar se o CLI headless depende de Fyne indiretamente.

### Fase 2 — Protótipo de Python relocável

- Baixar `python-build-standalone` para macOS.
- Instalar `openai-whisper` e `piper-tts`.
- Testar transcrição e síntese manualmente.
- Medir tamanho do Python após instalação.

### Fase 3 — Protótipo de FFmpeg relocável

- Baixar `ffmpeg` e `ffprobe` estáticos para macOS.
- Testar extração de áudio e remux com os binários em um diretório arbitrário.
- Confirmar que não dependem de bibliotecas dinâmicas externas.

### Fase 4 — Detecção de recursos em Go

- Implementar função de resolução de caminhos embarcados.
- Ajustar `config.go`, `environment/setup.go` e `audio/ffmpeg.go`.
- Adicionar testes unitários para os cenários:
  - dentro do `.app`
  - fora do `.app`
  - com variáveis de ambiente definidas

### Fase 5 — Script de empacotamento

- Criar `scripts/package-macos.sh`.
- Suportar arquitetura arm64 e x86_64.
- Gerar `Info.plist` com bundle ID apropriado (conforme `FyneApp.toml`).
- Empacotar CLI opcional em tarball separado.

### Fase 6 — Testes

- Executar o `.app` em uma máquina sem Python e sem FFmpeg no `PATH`.
- Processar um vídeo de exemplo do início ao fim.
- Confirmar que todos os intermediários são gerados.
- Testar o cancelamento de subprocessos.
- Testar o CLI empacotado.

### Fase 7 — Documentação

- Atualizar `README.md` com instruções de build do `.app`.
- Registrar tamanho do bundle, tempo de primeira execução e limitações de notarização.
- Documentar como gerar apenas o CLI headless.

## Tamanho esperado do bundle

- Python standalone com Whisper e Piper: ~250–400 MB.
- FFmpeg estático: ~50–100 MB.
- Binário Go: ~20–40 MB.
- Total estimado: ~350–550 MB.

## Riscos e limitações

- **Notarização**: sem Apple Developer ID, o usuário precisa autorizar o app em `Segurança e Privacidade`.
- **Primeira execução**: ainda pode ser lenta devido ao download do modelo Whisper e da voz Piper.
- **Tamanho**: Whisper e modelos aumentam significativamente o bundle se incluídos.
- **FFmpeg estático**: pode perder suporte a alguns codecs; validar com vídeos de exemplo.
- **Whisper**: modelo `large-v3` pode não caber em máquinas com pouca RAM; considerar modelo padrão menor para o bundle.
- **Atualizações**: mudanças nos pacotes Python ou no FFmpeg exigem novo build.

## Próximos passos recomendados

1. Iniciar pela Fase 2 (protótipo de Python relocável com Whisper e Piper).
2. Em paralelo, Fase 3 (protótipo de FFmpeg estático).
3. Depois de validados, aplicar a Fase 4 (detecção em Go) e Fase 5 (script de empacotamento).
