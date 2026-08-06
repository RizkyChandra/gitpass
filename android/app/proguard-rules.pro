# gomobile generates JNI-bound classes that are only reached from native code,
# so R8 sees no references and would strip them.
-keep class go.** { *; }
-keep class gitpass.** { *; }

# kotlinx.serialization resolves generated serializers reflectively; without
# these the app compiles and then fails at runtime the first time it decodes an
# entry, which is the worst possible place to find out.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**
-keepclassmembers class com.gitpass.** {
    *** Companion;
}
-keepclasseswithmembers class com.gitpass.** {
    kotlinx.serialization.KSerializer serializer(...);
}
-keep,includedescriptorclasses class com.gitpass.**$$serializer { *; }

# The autofill service and its unlock activity are instantiated by the platform
# from the manifest, never from our own code.
-keep class com.gitpass.autofill.GitpassAutofillService { *; }
-keep class com.gitpass.autofill.AutofillUnlockActivity { *; }
