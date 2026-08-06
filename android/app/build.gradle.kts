import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

/**
 * Signing credentials come from keystore.properties, which is gitignored, and
 * fall back to environment variables so CI can sign from secrets. When neither
 * is present the release build is simply left unsigned rather than failing —
 * a fresh clone should still be able to compile.
 */
val keystoreProperties = Properties().apply {
    val file = rootProject.file("keystore.properties")
    if (file.exists()) file.inputStream().use { load(it) }
}

fun signingValue(key: String, env: String): String? =
    keystoreProperties.getProperty(key) ?: System.getenv(env)

val releaseStoreFile = signingValue("storeFile", "GITPASS_KEYSTORE")
val releaseStorePassword = signingValue("storePassword", "GITPASS_KEYSTORE_PASSWORD")
val releaseKeyAlias = signingValue("keyAlias", "GITPASS_KEY_ALIAS")
val releaseKeyPassword = signingValue("keyPassword", "GITPASS_KEY_PASSWORD")
val canSignRelease = listOf(
    releaseStoreFile, releaseStorePassword, releaseKeyAlias, releaseKeyPassword,
).all { !it.isNullOrBlank() } && rootProject.file(releaseStoreFile!!).exists()

android {
    namespace = "com.gitpass"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.gitpass"
        // Autofill arrived in API 26; the Go .aar is built for 24 and up.
        minSdk = 26
        targetSdk = 35
        // Overridable from CI: -PversionName=1.3.0 -PversionCode=3
        versionCode = (project.findProperty("versionCode") as String?)?.toInt() ?: 3
        versionName = (project.findProperty("versionName") as String?) ?: "1.3.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"

        // The Go core ships as a native library, and gomobile only built these
        // two ABIs. Listing them keeps the APK off devices it would crash on.
        ndk {
            abiFilters += listOf("arm64-v8a", "x86_64")
        }
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    // No jvmToolchain: compile with whatever JDK runs Gradle, targeting 17.
    // Pinning a toolchain would make the build fail on machines that happen not
    // to have that exact JDK installed.
    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        }
    }

    signingConfigs {
        if (canSignRelease) {
            create("release") {
                storeFile = rootProject.file(releaseStoreFile!!)
                storePassword = releaseStorePassword
                keyAlias = releaseKeyAlias
                keyPassword = releaseKeyPassword
                // v1 (JAR signing) is only needed below API 24 and minSdk is 26,
                // so it is left off. v3 carries the rotation proof that lets
                // this key be replaced later without orphaning installs.
                enableV2Signing = true
                enableV3Signing = true
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            if (canSignRelease) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    packaging {
        resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"
    }
}

dependencies {
    // The Go core: crypto, storage and git, bound with gomobile.
    // Build it with `just aar` from the repo root.
    implementation(files("libs/gitpass.aar"))

    implementation(platform("androidx.compose:compose-bom:2024.10.01"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.biometric:biometric:1.1.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")

    debugImplementation("androidx.compose.ui:ui-tooling")

    testImplementation("junit:junit:4.13.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
}
