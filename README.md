# AI Video Dubber — Go/Fyne

Clone funcional do projeto `AI-Video-Dubber-py`, reimplementado com **Go** para a orquestração, **Fyne** para a interface gráfica e os mesmos componentes de IA do projeto original:

**Vídeo → áudio → Whisper → tradução → Piper TTS sincronizado → vídeo dublado**

A aplicação oferece uma interface desktop escura semelhante à versão Python, um CLI completo e comandos independentes para cada etapa do pipeline.

## Funcionalidades

- Interface gráfica em Fyne com seleção de vídeo, configuração da API, idioma, progresso em seis etapas, log e cancelamento.
- Extração de áudio e remux com FFmpeg.
- Transcrição local com OpenAI Whisper.
- Tradução por qualquer API compatível com OpenAI (`/v1/models` e `/v1/chat/completions`).
- Detecção automática do modelo quando o campo **Model** fica vazio.
- Síntese local com Piper TTS e download automático da voz necessária.
- Agrupamento de legendas para melhorar prosódia.
- Ajuste de `length_scale`, correção limitada com `atempo`, preenchimento e corte por janela temporal.
- Leitura de `.srt` e `.segments.txt` na síntese.
- Relatório JSON opcional com os parâmetros temporais de cada grupo.
- Reaproveitamento de intermediários no CLI e regeneração por `--force`.
- Cancelamento com encerramento da árvore de subprocessos no Unix.
- Escrita atômica dos principais artefatos para reduzir arquivos parcialmente gravados.
- Executável CLI separado, sem dependência do runtime gráfico.

> A aplicação é escrita em Go, mas Whisper e Piper continuam sendo executados em um ambiente Python gerenciado automaticamente. Isso preserva a compatibilidade e a qualidade do projeto de referência sem manter scripts Python próprios no projeto Go.

## Requisitos

- Go 1.23 ou mais recente.
- Python 3.10 ou mais recente.
- `ffmpeg` e `ffprobe` disponíveis no `PATH`.
- Compilador C e dependências nativas do Fyne para compilar a GUI.
- Uma API compatível com OpenAI para a tradução.

Na primeira execução, o programa cria `.venv`, atualiza as ferramentas de empacotamento e instala `openai-whisper` e `piper-tts`. A voz Piper selecionada também é baixada automaticamente.

### Linux (Debian/Ubuntu)

```bash
sudo apt update
sudo apt install -y golang python3 python3-venv ffmpeg gcc libgl1-mesa-dev xorg-dev
```

### macOS

```bash
xcode-select --install
brew install go python ffmpeg
```

### Windows

Instale Go, Python 3.10+, FFmpeg e um compilador C compatível com Fyne. Confirme no PowerShell:

```powershell
go version
python --version
ffmpeg -version
ffprobe -version
```

## Início rápido

### Linux/macOS

```bash
./scripts/build.sh
./bin/ai-video-dubber
```

### Windows PowerShell

```powershell
.\scripts\build.ps1
.\bin\ai-video-dubber.exe
```

Também é possível executar diretamente durante o desenvolvimento:

```bash
go run ./cmd/ai-video-dubber
```

## Interface gráfica

1. Selecione um vídeo.
2. Informe o endpoint da API de tradução.
3. Informe a chave, quando necessária.
4. Deixe **Model** vazio para detectar o primeiro modelo exposto pela API.
5. Escolha o idioma.
6. Clique em **Start Dubbing**.

A GUI sempre regenera os intermediários, reproduzindo o comportamento da interface do projeto Python. A chave da API não é persistida nas preferências da aplicação.

## CLI

O executável principal abre a GUI quando chamado sem argumentos e também oferece subcomandos:

```bash
./bin/ai-video-dubber dub --input video.mp4 --language pt-BR
```

Saída padrão:

```text
video.pt-BR.synced.mp4
```

Exemplo com API e modelo explícitos:

```bash
./bin/ai-video-dubber dub \
  --input video.mp4 \
  --language es \
  --api-base http://localhost:8000 \
  --api-key apikey \
  --model meu-modelo \
  --force
```

O binário headless aceita os mesmos subcomandos:

```bash
./bin/ai-video-dubber-cli dub --input video.mp4 --language fr
```

### Etapas independentes

```bash
# 1. Vídeo → MP3
./bin/ai-video-dubber-cli extract --input video.mp4

# 2. Áudio → SRT, segments, JSON e texto
./bin/ai-video-dubber-cli transcribe --input video.mp3 --model medium

# 3. SRT → SRT traduzido
./bin/ai-video-dubber-cli translate \
  --input video.srt \
  --output video.pt-BR.srt \
  --language pt-BR \
  --api-base http://localhost:8000

# 4. SRT/segments → áudio sincronizado
./bin/ai-video-dubber-cli synthesize \
  --input video.pt-BR.srt \
  --language pt-BR \
  --report-json video.pt-BR.tts-report.json

# 5. Vídeo + novo áudio → vídeo final
./bin/ai-video-dubber-cli merge \
  --video video.mp4 \
  --audio video.pt-BR.synced.mp3
```

Use `-h` após qualquer subcomando para ver todas as opções. O comando `synthesize` expõe controles avançados de agrupamento, Piper e correção temporal.

## Idiomas e vozes padrão

| Idioma | Código | Voz Piper |
|---|---:|---|
| Português do Brasil | `pt-BR` | `pt_BR-faber-medium` |
| Espanhol | `es` | `es_ES-davefx-medium` |
| Francês | `fr` | `fr_FR-siwis-medium` |
| Alemão | `de` | `de_DE-thorsten-medium` |
| Italiano | `it` | `it_IT-riccardo-x_low` |

No subcomando `synthesize`, `--voice` substitui a voz padrão.

## Configuração por ambiente

| Variável | Finalidade | Padrão |
|---|---|---|
| `LLM_API_BASE` | Endpoint OpenAI-compatible | `http://localhost:8000` |
| `LLM_API_KEY` | Chave da API | `apikey` |
| `LLM_MODEL` | Modelo de tradução | auto-detecção |
| `WHISPER_MODEL` | Modelo Whisper | `large-v3` |
| `PYTHON_BIN` | Python do sistema | `python3` ou `python` |
| `VENV_DIR` | Ambiente virtual gerenciado | `<projeto>/.venv` |
| `DATA_DIR` | Cache de vozes Piper | cache do usuário |
| `AI_VIDEO_DUBBER_HOME` | Diretório-base do aplicativo | detectado automaticamente |

Exemplo:

```bash
LLM_API_BASE=http://localhost:1234 \
LLM_API_KEY=local \
LLM_MODEL=qwen \
WHISPER_MODEL=medium \
./bin/ai-video-dubber-cli dub --input video.mp4 --language pt-BR
```

## Arquivos produzidos

Para `video.mp4` dublado em `pt-BR`:

```text
video.mp3
video.srt
video.segments.txt
video.json
video.txt
video.pt-BR.srt
video.pt-BR.synced.mp3
video.pt-BR.synced.mp4
```

O CLI ignora intermediários já existentes, salvo quando `--force` é usado. O vídeo final é sempre remontado.

## Desenvolvimento

```bash
# Baixar dependências
make deps

# Formatação, testes e vet em modo headless
make check

# Compilar GUI e CLI
make build

# Compilar apenas o CLI, inclusive em servidor sem desktop
make build-cli
```

Os testes da GUI usam a tag `ci`, que seleciona o driver de software do Fyne e não exige OpenGL/X11:

```bash
go test -tags ci ./...
go vet -tags ci ./...
```

A compilação nativa da GUI continua exigindo as dependências gráficas da plataforma.

## Estrutura

```text
.
├── assets/                       # ícone incorporado
├── cmd/
│   ├── ai-video-dubber/          # GUI + CLI
│   └── ai-video-dubber-cli/      # CLI headless
├── internal/
│   ├── audio/                    # FFmpeg, ffprobe, WAV e caminhos
│   ├── cli/                      # subcomandos
│   ├── config/                   # configuração e defaults
│   ├── environment/              # venv e dependências Python
│   ├── executil/                 # subprocessos, logs e cancelamento
│   ├── gui/                      # interface Fyne e tema
│   ├── language/                 # idiomas e vozes
│   ├── pipeline/                 # orquestração das seis etapas
│   ├── srt/                      # SRT e segments
│   ├── transcription/            # integração Whisper
│   ├── translation/              # cliente OpenAI-compatible
│   └── tts/                      # Piper e sincronização temporal
├── scripts/                      # build, execução e validação
├── FyneApp.toml
├── Makefile
└── go.mod
```

Veja também [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Limitações práticas

- `large-v3` exige bastante memória e pode ser lento sem GPU. Para testes, use `--whisper-model small` ou `medium`.
- A tradução envia o texto das legendas ao endpoint configurado; transcrição e TTS permanecem locais.
- O pipeline substitui a faixa de áudio principal e copia a faixa de vídeo. Faixas adicionais, capítulos e metadados não são preservados por padrão.
- A sincronização prioriza fala natural; quando um trecho não cabe na janela mesmo com correção limitada, o áudio é cortado com um pequeno fade-out.

## Licença

MIT. Consulte [`LICENSE`](LICENSE).
