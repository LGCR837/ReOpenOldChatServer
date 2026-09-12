# OcServer 编译脚本
# 用法: pwsh ./build.ps1

$ErrorActionPreference = "Stop"
$ProjectRoot = $PSScriptRoot
$EntryPkg = "./cmd/api"

Write-Host ""
Write-Host "===== OcServer Build Script =====" -ForegroundColor Cyan
Write-Host "  1. Windows amd64 调试 (根目录)"
Write-Host "  2. Linux   amd64 调试 (根目录, 静态)"
Write-Host "  3. 生产环境全平台编译 (build/)"
Write-Host ""
$choice = Read-Host "请选择 [1-3]"

switch ($choice) {
    "1" {
        Write-Host "`n>> 编译 Windows amd64 调试版本..." -ForegroundColor Yellow
        Push-Location $ProjectRoot
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue
        go build -o "ocserver_windows_amd64.exe" $EntryPkg
        Pop-Location
        if ($LASTEXITCODE -eq 0) {
            Write-Host ">> 完成: ocserver_windows_amd64.exe" -ForegroundColor Green
        } else {
            Write-Host ">> 编译失败" -ForegroundColor Red
            exit 1
        }
    }
    "2" {
        Write-Host "`n>> 交叉编译 Linux amd64 调试版本 (静态)..." -ForegroundColor Yellow
        Push-Location $ProjectRoot
        $env:GOOS = "linux"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        go build -o "ocserver_linux_amd64" $EntryPkg
        Pop-Location
        if ($LASTEXITCODE -eq 0) {
            Write-Host ">> 完成: ocserver_linux_amd64" -ForegroundColor Green
        } else {
            Write-Host ">> 编译失败" -ForegroundColor Red
            exit 1
        }
    }
    "3" {
        $version = Read-Host "`n请输入版本名称 (如 v1.0.0)"
        if ([string]::IsNullOrWhiteSpace($version)) {
            Write-Host "版本名称不能为空" -ForegroundColor Red
            exit 1
        }

        $BuildDir = Join-Path $ProjectRoot "build"
        if (!(Test-Path $BuildDir)) {
            New-Item -ItemType Directory -Path $BuildDir | Out-Null
        }

        # 目标平台列表 (Display 是文件名中使用的架构名)
        $targets = @(
            @{ GOOS = "windows"; GOARCH = "amd64"; Display = "amd64"; Ext = ".exe" },
            @{ GOOS = "windows"; GOARCH = "386";   Display = "i386";  Ext = ".exe" },
            @{ GOOS = "windows"; GOARCH = "arm64"; Display = "arm64"; Ext = ".exe" },
            @{ GOOS = "linux";   GOARCH = "amd64"; Display = "amd64"; Ext = ""    },
            @{ GOOS = "linux";   GOARCH = "386";   Display = "i386";  Ext = ""    },
            @{ GOOS = "linux";   GOARCH = "arm64"; Display = "arm64"; Ext = ""    },
            @{ GOOS = "linux";   GOARCH = "arm";   Display = "arm";   Ext = ""    }
        )

        $ldflags = "-s -w"
        $total = $targets.Count
        $idx = 0

        foreach ($t in $targets) {
            $idx++
            $os = $t.GOOS
            $arch = $t.GOARCH
            $display = $t.Display
            $ext = $t.Ext
            $outName = "ocserver_${os}_${display}_${version}${ext}"
            $outPath = Join-Path $BuildDir $outName

            Write-Host "`n[$idx/$total] 编译 $os/$arch -> $outName" -ForegroundColor Yellow

            $env:GOOS = $os
            $env:GOARCH = $arch
            $env:CGO_ENABLED = "0"

            Push-Location $ProjectRoot
            go build -ldflags $ldflags -o $outPath $EntryPkg
            Pop-Location

            if ($LASTEXITCODE -ne 0) {
                Write-Host "  编译失败: $os/$arch" -ForegroundColor Red
                exit 1
            }
            Write-Host "  完成" -ForegroundColor Green
        }

        # 清理环境变量
        Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
        Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
        Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue

        Write-Host "`n>> 全部编译完成, 产物在 build/ 目录:" -ForegroundColor Cyan
        Get-ChildItem $BuildDir | ForEach-Object {
            $sizeKB = [math]::Round($_.Length / 1KB)
            Write-Host ("  {0,-45} {1} KB" -f $_.Name, $sizeKB)
        }
    }
    default {
        Write-Host "无效选择" -ForegroundColor Red
        exit 1
    }
}
