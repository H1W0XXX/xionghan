package com.xionghan.app

import android.annotation.SuppressLint
import android.os.Bundle
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.appcompat.app.AppCompatActivity
import mobile.Mobile
import java.io.File
import java.io.FileOutputStream

class MainActivity : AppCompatActivity() {

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        // 1. 解压 web_mobile 目录 和 .onnx 模型到 filesDir
        val webDir = File(filesDir, "web_mobile")
        if (webDir.exists()) {
            webDir.deleteRecursively() // Force overwrite in development
        }
        copyAssetFolder("web_mobile", webDir)
        val modelFile = File(filesDir, "xionghan.onnx")
        if (!modelFile.exists()) {
            copyAssetFile("xionghan.onnx", modelFile)
        }

        // 2. 找到 libonnxruntime.so 的解压路径
        // 因为我们在 gradle 里放了 onnxruntime 的 AAR，系统会自动将其中的 .so 解压到 nativeLibraryDir
        var libPath = applicationInfo.nativeLibraryDir + "/libonnxruntime.so"
        val soFile = File(libPath)
        if (!soFile.exists()) {
            android.util.Log.e("GoLog", "CRITICAL: libonnxruntime.so does not exist at " + libPath)
            // Try fallback: sometimes it's in a subfolder or we can extract it manually, but for now just log it.
        } else {
            android.util.Log.i("GoLog", "Found libonnxruntime.so at " + libPath + " (size: " + soFile.length() + ")")
        }

        // 3. 启动后台 Go HTTP 服务
        val webPath = File(filesDir, "web_mobile").absolutePath
        val port = "2888"
        // 这里的 Mobile.startServer 就是之前用 gomobile bind 出来的包里的函数
        Mobile.startServer(webPath, modelFile.absolutePath, libPath, port)

        // 4. 配置 WebView
        val webView: WebView = findViewById(R.id.webview)
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            cacheMode = WebSettings.LOAD_NO_CACHE
        }
        
        webView.webViewClient = WebViewClient()

        // 延迟加载，等 Go 服务器启动完成
        webView.postDelayed({
            webView.loadUrl("http://127.0.0.1:$port/")
        }, 500)
    }

    private fun copyAssetFolder(assetFolder: String, targetFolder: File) {
        if (!targetFolder.exists()) {
            targetFolder.mkdirs()
        }
        val assetsList = assets.list(assetFolder) ?: return
        for (assetName in assetsList) {
            val assetPath = "$assetFolder/$assetName"
            val targetFile = File(targetFolder, assetName)
            // 如果是目录，assets.list 仍然能拿到，但是需要判断是否是文件
            val isDir = (assets.list(assetPath)?.size ?: 0) > 0
            if (isDir) {
                copyAssetFolder(assetPath, targetFile)
            } else {
                copyAssetFile(assetPath, targetFile)
            }
        }
    }

    private fun copyAssetFile(assetFilePath: String, targetFile: File) {
        if (targetFile.exists()) return
        try {
            assets.open(assetFilePath).use { inputStream ->
                FileOutputStream(targetFile).use { outputStream ->
                    inputStream.copyTo(outputStream)
                }
            }
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }
}