category: Fixed
- **Trip assistant changes no longer lost over a hotel name** - The assistant sometimes wrote a plain field (a hotel name, phone number) as a three-language text, and the whole change was rejected ("cannot unmarshal object into ... stays.0.name"). Such fields are now read as their Norwegian/English text, and the assistant is told which fields are plain strings.
