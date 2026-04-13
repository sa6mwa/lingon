import com.android.build.api.dsl.ManagedVirtualDevice
import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
    id("com.google.protobuf")
}

private val integrationRunnerArgProperties = mapOf(
    "class" to "lingon.it.class",
    "endpoint" to "lingon.it.endpoint",
    "username" to "lingon.it.username",
    "password" to "lingon.it.password",
    "totp_secret" to "lingon.it.totp_secret",
    "ca_pem_b64" to "lingon.it.ca_pem_b64",
    "sessions" to "lingon.it.sessions",
    "view_token" to "lingon.it.view_token",
    "username2" to "lingon.it.username2",
    "password2" to "lingon.it.password2",
    "totp_secret2" to "lingon.it.totp_secret2",
    "sessions2" to "lingon.it.sessions2",
    "view_token2" to "lingon.it.view_token2",
    "host_cols" to "lingon.it.host_cols",
    "host_rows" to "lingon.it.host_rows",
)

android {
    namespace = "systems.pkt.lingon"
    compileSdk = 36

    defaultConfig {
        applicationId = "systems.pkt.lingon"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "v0.1.1-0.20260413112426-0497d225b48a"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        integrationRunnerArgProperties.forEach { (runnerArg, gradleProperty) ->
            providers.gradleProperty(gradleProperty).orNull?.let { value ->
                testInstrumentationRunnerArguments[runnerArg] = value
            }
        }

        vectorDrawables {
            useSupportLibrary = true
        }
    }

    val signingPropsFile = rootProject.file("signing.properties")
    val signingProps = Properties()
    val hasSigningProps = signingPropsFile.isFile
    if (hasSigningProps) {
        signingPropsFile.inputStream().use { signingProps.load(it) }
        signingConfigs {
            create("release") {
                storeFile = file(signingProps.getProperty("storeFile"))
                storePassword = signingProps.getProperty("storePassword")
                keyAlias = signingProps.getProperty("keyAlias")
                keyPassword = signingProps.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (hasSigningProps) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    testOptions {
        managedDevices {
            allDevices {
                maybeCreate<ManagedVirtualDevice>("phoneApi35").apply {
                    device = "Medium Phone"
                    apiLevel = 35
                    systemImageSource = "aosp"
                    testedAbi = "x86_64"
                }
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlin {
        compilerOptions {
            jvmTarget.set(JvmTarget.JVM_17)
        }
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.17.0")
    implementation("androidx.activity:activity-compose:1.12.2")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.9.4")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.4")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.4")
    implementation("androidx.datastore:datastore-preferences:1.2.0")
    implementation("androidx.work:work-runtime-ktx:2.10.5")

    implementation(platform("androidx.compose:compose-bom:2026.01.00"))
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material:material")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.foundation:foundation")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("com.google.android.material:material:1.13.0")

    implementation("com.squareup.okhttp3:okhttp:5.3.2")
    implementation("com.squareup.okhttp3:okhttp-sse:5.3.2")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
    implementation("com.google.protobuf:protobuf-javalite:4.33.4")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
    testImplementation("com.squareup.okhttp3:mockwebserver:5.3.2")

    androidTestImplementation("androidx.test.ext:junit:1.3.0")
    androidTestImplementation("androidx.test:runner:1.7.0")
    androidTestImplementation("androidx.test:rules:1.7.0")
    androidTestImplementation("androidx.test.espresso:espresso-core:3.7.0")
    androidTestImplementation(platform("androidx.compose:compose-bom:2026.01.00"))
    androidTestImplementation("androidx.compose.ui:ui-test-junit4")

    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}

protobuf {
    protoc {
        artifact = "com.google.protobuf:protoc:4.33.4"
    }
    generateProtoTasks {
        all().configureEach {
            builtins {
                maybeCreate("java").apply {
                    option("lite")
                }
            }
        }
    }
}
