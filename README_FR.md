# silentjson : Le parseur JSON haute performance pour Go

`silentjson` est une bibliothèque JSON pour Go hautement optimisée, sans réflexion и sans allocation, qui offre des performances extrêmes **sans nécessiter de génération de code.**

## 🚀 Pourquoi `silentjson` ?

- **Jusqu'à 30x plus rapide :** Pour les grands tableaux JSON, `UnmarshalArrayParallel` exploite tous les cœurs de votre processeur, atteignant des vitesses supérieures à 12 Go/s.
- **Zéro génération de code :** Contrairement à d'autres bibliothèques, aucun `go generate` n'est nécessaire. Cela fonctionne instantanément.
- **Architecture Zero-Copy :** Les chaînes de caractères sont mappées directement depuis le tampon d'entrée, minimisant la pression sur le ramasse-miettes (GC).

## 📊 Performances (AMD Ryzen 9 7950X3D)

### Comparaison d'architecture (100k objets)
| Mode | Débit (Mo/s) |
| :--- | :--- |
| **SilentJSON (AVX2)** | **24 670 Mo/s** ⭐ |
| **SilentJSON (Scalaire)** | **810 Mo/s** |
| **Sonic (JIT)** | 644 Mo/s |
| **Standard (Go)** | 110 Mo/s |

## ⚙️ Caractéristiques principales
- **Accélération SIMD :** Utilise AVX2 sur `amd64` pour un traitement ultra-rapide.
- **Support ARM64 :** Support expérimental pour Apple Silicon et Linux ARM.
- **Shared Memory (SHM) :** Idéal pour l'IPC à faible latence (Zero-Copy).
- **Streaming :** Décodage efficace de flux massifs via `io.Reader`.

## 📦 Installation
```bash
go get github.com/GenshIv/silentjson
```

## 🛠️ Utilisation rapide

### 1. Créer le registre (une seule fois)
```go
var empRegistry = silentjson.BuildRegistry(reflect.TypeOf(Employee{}))
```

### 2. Désérialisation parallèle
```go
employees := make([]Employee, count)
employees, err := silentjson.UnmarshalArrayParallel[Employee](rawJSON, empRegistry, employees)
```

### 3. IPC via Mémoire Partagée (SHM)
```go
// Décodage direct depuis un segment SHM sans allocation sur le tas
err := silentjson.ParseObject(shmPayload, reg, unsafe.Pointer(&trade))
```

## 📄 Licence
Sous licence MIT. Voir le fichier [LICENSE](LICENSE) pour plus de détails.
