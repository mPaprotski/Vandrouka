import {useState} from 'react';
import {Pressable,StyleSheet,Text,TextInput,View} from 'react-native';
import {colors} from './theme';

export function PasswordInput({label,value,onChangeText,editable=true,newPassword=false}:{
 label:string;value:string;onChangeText:(value:string)=>void;editable?:boolean;newPassword?:boolean;
}){
 const [visible,setVisible]=useState(false);
 return <View style={styles.field}>
  <TextInput accessibilityLabel={label} value={value} onChangeText={onChangeText}
   editable={editable} secureTextEntry={!visible} autoCapitalize="none" autoCorrect={false}
   autoComplete={newPassword?'new-password':'current-password'}
   textContentType={newPassword?'newPassword':'password'} style={styles.input}/>
  <Pressable accessibilityRole="button" accessibilityLabel={`${visible?'Скрыть':'Показать'} содержимое поля «${label}»`}
   accessibilityState={{disabled:!editable}} disabled={!editable}
   onPress={()=>setVisible(value=>!value)} style={({pressed})=>[styles.toggle,pressed&&styles.pressed]}>
   <Text style={styles.toggleText}>{visible?'Скрыть':'Показать'}</Text>
  </Pressable>
 </View>;
}
const styles=StyleSheet.create({
 field:{flexDirection:'row',alignItems:'center',backgroundColor:colors.surface,borderColor:colors.border,borderWidth:1,borderRadius:14},
 input:{flex:1,minWidth:0,padding:16,minHeight:56,fontSize:16,color:colors.primary},
 toggle:{minHeight:56,minWidth:80,paddingHorizontal:12,alignItems:'center',justifyContent:'center'},
 toggleText:{fontSize:13,fontWeight:'600',color:colors.primary},pressed:{opacity:.65},
});
