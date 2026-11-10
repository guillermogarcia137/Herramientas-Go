# Herramientas-Go

Diez utilidades en un ejecutable, sin dependencias externas. Windows y Linux.
Requiere Go 1.24 o posterior para compilar.

```console
go test ./...
go build -o dist/ ./...
```

En Windows ejecuta `dist\Herramientas-Go.exe --help`; en Linux,
`./dist/Herramientas-Go --help`. Los artefactos de GitHub Actions contienen el
binario compilado para cada sistema cuando las comprobaciones terminan.

| Comando | Ejemplo de argumentos | Función |
| --- | --- | --- |
| `hash` | `archivo.iso` | SHA-256 en bloques |
| `duplicados` | `C:\Datos` | Agrupa archivos idénticos sin eliminar |
| `backup` | `C:\Datos D:\Copias\datos.zip` | ZIP nuevo, incluidas carpetas vacías |
| `manifiesto` | `C:\Datos D:\Copias\datos.json` | Inventario de tamaños y huellas |
| `verificar` | `C:\Datos D:\Copias\datos.json` | Detecta archivos ausentes o alterados |
| `grandes` | `C:\Datos 20` | Los veinte archivos de mayor tamaño |
| `tcp` | `127.0.0.1 8000` | Comprueba conexión TCP |
| `http` | `http://127.0.0.1:8000` | Cabeceras por HEAD con timeout |
| `servir` | `C:\Proyecto 8000` | Servidor local en primer plano |
| `csv-json` | `datos.csv datos.json` | CSV con cabeceras a JSON |

Pon las rutas con espacios entre comillas. Las carpetas de salida deben existir.
Las salidas se crean de forma exclusiva y no sobrescriben archivos. Una copia
ZIP o una escritura JSON fallida elimina su salida parcial. Las copias no
preservan ACL ni garantizan consistencia de archivos que estén cambiando.
Los recorridos omiten enlaces simbólicos; no son una defensa frente a procesos
que modifiquen rutas simultáneamente. El manifiesto debe guardarse fuera del origen.
`verificar` comprueba las entradas del manifiesto: no denuncia archivos añadidos.

`servir` se limita a loopback y rechaza enlaces que resuelvan fuera del origen.
Elige una carpeta sin secretos: publica archivos e índices para otros procesos
de tu equipo. Ctrl+C detiene el servidor. No es un servidor de producción.
Las peticiones HTTP verifican TLS y utilizan las opciones de proxy de Go.
Los CSV se cargan en memoria y conservan sus celdas como cadenas; no se ejecutan.
Los informes pueden incluir rutas y otros datos privados: revisa antes de compartir.

Pruebas con archivos temporales: integridad ZIP, preservación de destinos,
manifiestos alterados, CSV, cabeceras HTTP locales y límites de enlaces del servidor.
